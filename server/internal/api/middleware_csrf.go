package api

import (
	"net/http"
	"net/url"
	"strings"
)

// csrfProtect guards state-modifying requests that rely on ambient credentials
// (cookie sessions) against Cross-Site Request Forgery. API tokens and webhooks
// are unaffected.
func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Safe methods do not mutate state
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			next.ServeHTTP(w, r)
			return
		}

		// Only requests authenticated via browser session cookies need CSRF checking
		id, ok := IdentityFrom(r.Context())
		if !ok || id.ViaToken() {
			next.ServeHTTP(w, r)
			return
		}

		// Verify Origin or Referer header against AppURL and Host
		origin := r.Header.Get("Origin")
		if origin == "" {
			if ref := r.Header.Get("Referer"); ref != "" {
				if parsed, err := url.Parse(ref); err == nil {
					origin = parsed.Scheme + "://" + parsed.Host
				}
			}
		}

		if origin != "" {
			parsedOrigin, err := url.Parse(origin)
			if err == nil {
				// Allow if matches AppURL host, or matches r.Host, or is localhost/127.0.0.1 in non-production
				expectedHost := r.Host
				if parsedAppURL, err := url.Parse(s.Config.AppURL); err == nil && parsedAppURL.Host != "" {
					expectedHost = parsedAppURL.Host
				}

				if strings.EqualFold(parsedOrigin.Host, r.Host) ||
					strings.EqualFold(parsedOrigin.Host, expectedHost) ||
					(!s.Config.IsProduction() && (strings.HasPrefix(parsedOrigin.Host, "localhost") || strings.HasPrefix(parsedOrigin.Host, "127.0.0.1"))) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		// If Sec-Fetch-Site is present and not cross-site, allow
		if sfs := r.Header.Get("Sec-Fetch-Site"); sfs == "same-origin" || sfs == "same-site" || sfs == "none" {
			next.ServeHTTP(w, r)
			return
		}

		Wrap(s.Log, func(http.ResponseWriter, *http.Request) error {
			return Forbidden("Cross-site request forgery protection: origin validation failed.")
		})(w, r)
	})
}
