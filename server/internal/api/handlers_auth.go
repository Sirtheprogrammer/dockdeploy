package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// invitationLifetime bounds how long a handed-out link stays usable.
const invitationLifetime = 7 * 24 * time.Hour

// dummyHash is verified against when no user matches, so a wrong email and a
// wrong password take the same time to reject and the sign-in form cannot be
// used to enumerate accounts.
var dummyHash string

func init() {
	hash, err := auth.HashPassword("dummy-password-for-timing-parity")
	if err != nil {
		panic("api: cannot hash timing placeholder: " + err.Error())
	}
	dummyHash = hash
}

type userResponse struct {
	store.User
	Permissions []auth.Permission `json:"permissions"`
}

func newUserResponse(u store.User) userResponse {
	return userResponse{User: u, Permissions: u.Role.Permissions()}
}

// --- first-run setup ----------------------------------------------------

type setupStatusResponse struct {
	Needed bool `json:"needed"`
}

// handleSetupStatus tells the dashboard whether to show the setup screen or
// the sign-in form.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) error {
	count, err := s.Store.CountUsers(r.Context())
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, setupStatusResponse{Needed: count == 0})
}

type setupRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleSetup creates the first administrator. It is reachable without
// authentication precisely once: the store refuses a second call.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) error {
	var req setupRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	email := f.email("email", req.Email)
	name := f.required("name", req.Name, 1, 100)
	if err := auth.ValidatePassword(req.Password); err != nil {
		f.add("password", err.Error())
	}
	if err := f.err(); err != nil {
		return err
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return Internal(err)
	}

	user, err := s.Store.CreateFirstAdmin(r.Context(), email, name, hash)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Conflict("This instance has already been set up.")
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "users", user.ID)
	AuditMeta(r.Context(), "event", "first_admin_created")
	s.Log.Info("first administrator created", "email", user.Email)

	// In local zero-dependency mode, auto-detect local Docker socket and register local server.
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		localServer, err := s.Store.CreateServer(r.Context(), s.Sealer, store.NewServer{
			Name:               "Local Docker",
			Host:               "localhost",
			Port:               0,
			Username:           "local",
			AuthMethod:         sshx.AuthLocal,
			HostKeyFingerprint: "local",
			DockerSocket:       "/var/run/docker.sock",
			CreatedBy:          user.ID,
		})
		if err == nil {
			s.Log.Info("auto-registered local Docker server", "id", localServer.ID)
			if caps, probeErr := s.Servers.Probe(r.Context(), localServer); probeErr == nil {
				s.Log.Info("local Docker probe successful", "docker_version", caps.DockerVersion)
				if s.Discovery != nil {
					go func() {
						discCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						_, _ = s.Discovery.DiscoverServer(discCtx, localServer, user.ID)
					}()
				}
			}
		}
	}

	return s.startSession(w, r, user, http.StatusCreated)
}

// --- sign in and out ----------------------------------------------------

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.Email == "" || req.Password == "" {
		return Invalid(fields{"email": "Enter your email and password."})
	}

	AuditMeta(r.Context(), "email", store.NormaliseEmail(req.Email))

	user, err := s.Store.UserByEmail(r.Context(), req.Email)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return Internal(err)
		}
		// Burn the same work an existing account would, then fail identically.
		_ = auth.VerifyPassword(req.Password, dummyHash)
		return Unauthorized("Email or password is incorrect.")
	}

	if err := auth.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		if errors.Is(err, auth.ErrInvalidHash) {
			s.Log.Error("stored password hash is unreadable", "user", user.ID)
		}
		return Unauthorized("Email or password is incorrect.")
	}

	if !user.IsActive() {
		return Forbidden("This account has been suspended.")
	}

	if user.TOTPEnabled {
		tempToken := s.Hasher.Sign2FAToken(user.ID, time.Now().Add(5*time.Minute))
		return JSON(w, s.Log, http.StatusOK, map[string]any{
			"requires_2fa": true,
			"temp_token":   tempToken,
		})
	}

	if err := s.Store.TouchUserLogin(r.Context(), user.ID); err != nil {
		s.Log.Warn("record login time", "error", err)
	}
	return s.startSession(w, r, user, http.StatusOK)
}

type login2FARequest struct {
	TempToken string `json:"temp_token"`
	Code      string `json:"code"`
}

func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) error {
	var req login2FARequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}
	req.TempToken = strings.TrimSpace(req.TempToken)
	req.Code = strings.TrimSpace(req.Code)
	if req.TempToken == "" || req.Code == "" {
		return Invalid(fields{"code": "Please enter your authentication code."})
	}

	userID, err := s.Hasher.Verify2FAToken(req.TempToken)
	if err != nil {
		return Unauthorized("Your 2FA login session has expired or is invalid. Please log in again.")
	}

	user, err := s.Store.UserByID(r.Context(), userID)
	if err != nil {
		return Unauthorized("User not found.")
	}
	if !user.IsActive() {
		return Forbidden("This account has been suspended.")
	}
	if !user.TOTPEnabled {
		return Unauthorized("2FA is not enabled for this account.")
	}

	verified := false
	if len(req.Code) == auth.TOTPDigits {
		secret, err := s.Store.GetUserTOTPSecret(r.Context(), s.Sealer, user)
		if err == nil && secret != "" {
			if auth.VerifyTOTP(secret, req.Code, time.Now()) {
				verified = true
			}
		}
	}

	if !verified {
		consumed, err := s.Store.ConsumeRecoveryCode(r.Context(), s.Sealer, user, req.Code)
		if err == nil && consumed {
			verified = true
			s.Log.Info("user authenticated with 2fa recovery code", "user", user.ID)
		}
	}

	if !verified {
		return Unauthorized("Invalid authentication code or recovery code.")
	}

	if err := s.Store.TouchUserLogin(r.Context(), user.ID); err != nil {
		s.Log.Warn("record login time", "error", err)
	}

	return s.startSession(w, r, user, http.StatusOK)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	if id.SessionID != "" {
		if err := s.Store.DeleteSession(r.Context(), id.SessionID); err != nil {
			return Internal(err)
		}
	}
	s.clearSessionCookie(w)
	return NoContent(w)
}

// startSession issues a cookie and returns the signed-in user.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user *store.User, status int) error {
	token, hash, err := s.Hasher.NewToken()
	if err != nil {
		return Internal(err)
	}
	expires := time.Now().Add(sessionLifetime)

	if _, err := s.Store.CreateSession(r.Context(), user.ID, hash, expires,
		clientIP(r), r.UserAgent()); err != nil {
		return Internal(err)
	}

	s.setSessionCookie(w, token, expires)

	// The audit middleware reads the identity from the request context, which
	// this request does not have yet -- it is the one creating it. Name the
	// actor explicitly so sign-ins are attributable.
	AuditResource(r.Context(), "users", user.ID)
	AuditMeta(r.Context(), "user_id", user.ID)

	return JSON(w, s.Log, status, newUserResponse(*user))
}

// --- the signed-in user -------------------------------------------------

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) error {
	return JSON(w, s.Log, http.StatusOK, newUserResponse(MustIdentity(r.Context()).User))
}

type updateMeRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) error {
	var req updateMeRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}
	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	if err := f.err(); err != nil {
		return err
	}

	id := MustIdentity(r.Context())
	user, err := s.Store.UpdateUserName(r.Context(), id.User.ID, name)
	if err != nil {
		return Internal(err)
	}
	AuditResource(r.Context(), "users", user.ID)
	return JSON(w, s.Log, http.StatusOK, newUserResponse(*user))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword rotates the password and ends every other session, so
// changing it after a suspected leak actually ejects the other party.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) error {
	var req changePasswordRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		f.add("new_password", err.Error())
	}
	if err := f.err(); err != nil {
		return err
	}

	id := MustIdentity(r.Context())
	if err := auth.VerifyPassword(req.CurrentPassword, id.User.PasswordHash); err != nil {
		return Invalid(fields{"current_password": "That is not your current password."})
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return Internal(err)
	}
	if err := s.Store.UpdateUserPassword(r.Context(), id.User.ID, hash, id.SessionID); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "users", id.User.ID)
	AuditMeta(r.Context(), "event", "password_changed")
	return NoContent(w)
}

// --- Two-Factor Authentication (2FA / TOTP) ----------------------------

func (s *Server) handle2FAStatus(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	user, err := s.Store.UserByID(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"enabled": user.TOTPEnabled,
	})
}

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return Internal(err)
	}

	if err := s.Store.SetUserTOTPSecret(r.Context(), s.Sealer, id.User.ID, secret); err != nil {
		return Internal(err)
	}

	otpauthURL := auth.GenerateTOTPURL("dockdeploy", id.User.Email, secret)

	return JSON(w, s.Log, http.StatusOK, map[string]string{
		"secret":      secret,
		"otpauth_url": otpauthURL,
	})
}

type enable2FARequest struct {
	Code string `json:"code"`
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) error {
	var req enable2FARequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}
	req.Code = strings.TrimSpace(req.Code)
	if len(req.Code) != auth.TOTPDigits {
		return Invalid(fields{"code": "Enter a valid 6-digit code from your authenticator app."})
	}

	id := MustIdentity(r.Context())
	user, err := s.Store.UserByID(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}

	secret, err := s.Store.GetUserTOTPSecret(r.Context(), s.Sealer, user)
	if err != nil || secret == "" {
		return BadRequest("2FA setup has not been initiated. Please start setup first.")
	}

	if !auth.VerifyTOTP(secret, req.Code, time.Now()) {
		return Invalid(fields{"code": "The code does not match. Check the time on your device and try again."})
	}

	recoveryCodes, err := auth.GenerateRecoveryCodes(10)
	if err != nil {
		return Internal(err)
	}

	if err := s.Store.EnableUserTOTP(r.Context(), s.Sealer, user.ID, recoveryCodes); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "users", user.ID)
	AuditMeta(r.Context(), "event", "2fa_enabled")
	s.Log.Info("2fa enabled", "user", user.ID)

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"success":        true,
		"recovery_codes": recoveryCodes,
	})
}

type disable2FARequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) error {
	var req disable2FARequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	id := MustIdentity(r.Context())
	user, err := s.Store.UserByID(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}

	if !user.TOTPEnabled {
		return BadRequest("2FA is not enabled on this account.")
	}

	verified := false
	if req.Password != "" {
		if err := auth.VerifyPassword(req.Password, user.PasswordHash); err == nil {
			verified = true
		}
	}
	if !verified && req.Code != "" {
		secret, err := s.Store.GetUserTOTPSecret(r.Context(), s.Sealer, user)
		if err == nil && secret != "" && auth.VerifyTOTP(secret, req.Code, time.Now()) {
			verified = true
		}
	}

	if !verified {
		return Invalid(fields{"password": "Valid current password or authenticator code is required to disable 2FA."})
	}

	if err := s.Store.DisableUserTOTP(r.Context(), user.ID); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "users", user.ID)
	AuditMeta(r.Context(), "event", "2fa_disabled")
	s.Log.Info("2fa disabled", "user", user.ID)

	return NoContent(w)
}

type regenerateRecoveryCodesRequest struct {
	Password string `json:"password"`
}

func (s *Server) handle2FARegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) error {
	var req regenerateRecoveryCodesRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	id := MustIdentity(r.Context())
	user, err := s.Store.UserByID(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}

	if !user.TOTPEnabled {
		return BadRequest("2FA is not enabled on this account.")
	}

	if err := auth.VerifyPassword(req.Password, user.PasswordHash); err != nil {
		return Invalid(fields{"password": "That is not your current password."})
	}

	recoveryCodes, err := auth.GenerateRecoveryCodes(10)
	if err != nil {
		return Internal(err)
	}

	if err := s.Store.EnableUserTOTP(r.Context(), s.Sealer, user.ID, recoveryCodes); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "users", user.ID)
	AuditMeta(r.Context(), "event", "2fa_recovery_codes_regenerated")
	s.Log.Info("2fa recovery codes regenerated", "user", user.ID)

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"recovery_codes": recoveryCodes,
	})
}

type sessionResponse struct {
	ID         string    `json:"id"`
	Current    bool      `json:"current"`
	IP         *string   `json:"ip"`
	UserAgent  *string   `json:"user_agent"`
	LastUsedAt time.Time `json:"last_used_at"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	sessions, err := s.Store.ListSessions(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}

	out := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, sessionResponse{
			ID:         session.ID,
			Current:    session.ID == id.SessionID,
			IP:         session.IP,
			UserAgent:  session.UserAgent,
			LastUsedAt: session.LastUsedAt,
			CreatedAt:  session.CreatedAt,
		})
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"sessions": out})
}

// handleRevokeSession signs out one other device.
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	target := chi.URLParam(r, "sessionID")

	// Scoped to the caller, so a session id belonging to someone else cannot
	// be revoked by guessing it.
	sessions, err := s.Store.ListSessions(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}
	for _, session := range sessions {
		if session.ID != target {
			continue
		}
		if err := s.Store.DeleteSession(r.Context(), target); err != nil {
			return Internal(err)
		}
		if target == id.SessionID {
			s.clearSessionCookie(w)
		}
		return NoContent(w)
	}
	return NotFound("No such session.")
}

// --- invitations --------------------------------------------------------

type invitationPreview struct {
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  auth.Role `json:"role"`
}

// handleInvitationPreview lets the accept page show who the link is for before
// asking for a password. It is public by necessity: the token is the only
// credential the recipient has.
func (s *Server) handleInvitationPreview(w http.ResponseWriter, r *http.Request) error {
	invitation, err := s.lookupInvitation(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		return err
	}
	return JSON(w, s.Log, http.StatusOK, invitationPreview{
		Email: invitation.Email, Name: invitation.Name, Role: invitation.Role,
	})
}

type acceptInvitationRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) error {
	var req acceptInvitationRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	invitation, err := s.lookupInvitation(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		return err
	}

	f := fields{}
	name := req.Name
	if name == "" {
		name = invitation.Name
	}
	name = f.required("name", name, 1, 100)
	if err := auth.ValidatePassword(req.Password); err != nil {
		f.add("password", err.Error())
	}
	if err := f.err(); err != nil {
		return err
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return Internal(err)
	}

	user, err := s.Store.AcceptInvitation(r.Context(), invitation.ID, name, hash)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			return NotFound("This invitation is no longer valid.")
		case errors.Is(err, store.ErrConflict):
			return Conflict("An account with that email already exists.")
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "users", user.ID)
	AuditMeta(r.Context(), "event", "invitation_accepted")
	return s.startSession(w, r, user, http.StatusCreated)
}

func (s *Server) lookupInvitation(ctx context.Context, token string) (*store.Invitation, error) {
	if token == "" {
		return nil, NotFound("This invitation link is not valid.")
	}
	invitation, err := s.Store.InvitationByTokenHash(ctx, s.Hasher.Hash(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotFound("This invitation link has expired or has already been used.")
		}
		return nil, Internal(err)
	}
	return invitation, nil
}
