package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsCredentialAttempt(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/auth/login", true},
		{http.MethodPost, "/api/auth/login/2fa", true},
		{http.MethodPost, "/api/auth/setup", true},
		{http.MethodPost, "/api/auth/password", true},
		{http.MethodPost, "/api/auth/invitations/abc/accept", true},
		{http.MethodGet, "/api/auth/invitations/abc", true},
		{http.MethodGet, "/api/auth/me", false},
		{http.MethodGet, "/api/auth/setup", false},
		{http.MethodGet, "/api/auth/sessions", false},
		{http.MethodGet, "/api/auth/2fa/status", false},
		{http.MethodPost, "/api/auth/logout", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := isCredentialAttempt(r); got != c.want {
			t.Errorf("%s %s: got %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
