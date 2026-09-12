package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

const (
	// sessionCookieName is prefixed with __Host- only when the deployment is
	// actually on HTTPS; the prefix requires Secure, and setting it over plain
	// HTTP makes browsers drop the cookie silently.
	sessionCookieName       = "dockdeploy_session"
	secureSessionCookieName = "__Host-dockdeploy_session"

	// sessionLifetime is a sliding window, refreshed while the user is active.
	sessionLifetime = 14 * 24 * time.Hour
	// sessionSlideAfter avoids a database write on every single request.
	sessionSlideAfter = time.Hour
)

func (s *Server) cookieName() string {
	if s.secureCookies() {
		return secureSessionCookieName
	}
	return sessionCookieName
}

// secureCookies follows the configured public URL rather than APP_ENV, because
// plenty of self-hosted instances run APP_ENV=production behind a plain-HTTP
// reverse proxy on a private network. Marking the cookie Secure there would
// make sign-in fail with no visible reason.
func (s *Server) secureCookies() bool {
	return strings.HasPrefix(s.Config.AppURL, "https://")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

// authenticate resolves credentials into an Identity but never rejects a
// request on its own. Authorization is the job of requirePermission, so that
// public routes and guarded routes share one code path here.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := s.identify(r); id != nil {
			r = r.WithContext(withIdentity(r.Context(), id))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) identify(r *http.Request) *Identity {
	ctx := r.Context()
	log := LoggerFrom(ctx)

	if token, ok := auth.ParseBearer(r.Header.Get("Authorization")); ok {
		found, err := s.Store.APITokenByHash(ctx, s.Hasher.Hash(token))
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				log.Error("resolve api token", "error", err)
			}
			return nil
		}
		// Recording use is best-effort; a failure here must not block a deploy.
		if err := s.Store.TouchAPIToken(ctx, found.Token.ID); err != nil {
			log.Warn("touch api token", "error", err)
		}
		return &Identity{User: found.User, TokenID: found.Token.ID}
	}

	cookie, err := r.Cookie(s.cookieName())
	if err != nil || cookie.Value == "" {
		return nil
	}

	found, err := s.Store.SessionByTokenHash(ctx, s.Hasher.Hash(cookie.Value))
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			log.Error("resolve session", "error", err)
		}
		return nil
	}

	if time.Since(found.Session.LastUsedAt) > sessionSlideAfter {
		if err := s.Store.TouchSession(ctx, found.Session.ID, time.Now().Add(sessionLifetime)); err != nil {
			log.Warn("slide session expiry", "error", err)
		}
	}
	return &Identity{User: found.User, SessionID: found.Session.ID}
}

// requirePermission enforces the policy a route declared at registration.
func (s *Server) requirePermission(perm auth.Permission, next Handler) Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, ok := IdentityFrom(r.Context())
		if !ok {
			return Unauthorized("Sign in to continue.")
		}
		if !id.User.IsActive() {
			return Forbidden("This account has been suspended.")
		}
		if !id.Can(perm) {
			LoggerFrom(r.Context()).Info("permission denied",
				"user", id.User.Email, "role", id.Role(), "permission", perm)
			return Forbidden("Your role does not allow this.")
		}
		return next(w, r)
	}
}
