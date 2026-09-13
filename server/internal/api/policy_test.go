package api

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
)

func noop(http.ResponseWriter, *http.Request) error { return nil }

func testServer() *Server {
	return &Server{
		Log:      slog.New(slog.DiscardHandler),
		policies: map[string]policy{},
	}
}

// The startup assertion is the backstop for the entire authorization model:
// if it stops catching unguarded routes, a forgotten permission ships silently.
func TestAssertPoliciesRejectsUnguardedRoute(t *testing.T) {
	s := testServer()
	r := chi.NewRouter()

	routes{server: s, router: r}.group("/api", func(api routes) {
		api.guarded(http.MethodGet, "/servers", auth.PermServerRead, noop)
		// Registered straight on chi, bypassing the helpers. This is exactly
		// the mistake the assertion exists to catch.
		api.router.Get("/secrets", s.wrap(noop))
	})

	err := assertPolicies(r, s.policies)
	if err == nil {
		t.Fatal("assertPolicies accepted a route with no policy")
	}
	if !strings.Contains(err.Error(), "GET /api/secrets") {
		t.Errorf("error = %q, want it to name GET /api/secrets", err)
	}
}

func TestAssertPoliciesAcceptsFullyDeclaredRouter(t *testing.T) {
	s := testServer()
	r := chi.NewRouter()

	routes{server: s, router: r}.group("/api", func(api routes) {
		api.open(http.MethodGet, "/health", noop)
		api.guarded(http.MethodGet, "/audit", auth.PermAuditRead, noop)

		api.group("/users", func(u routes) {
			u.guarded(http.MethodGet, "/", auth.PermUserRead, noop)
			u.guarded(http.MethodDelete, "/{userID}", auth.PermUserWrite, noop)
		})
	})

	if err := assertPolicies(r, s.policies); err != nil {
		t.Fatalf("assertPolicies = %v, want nil", err)
	}
}

// The recorded keys have to match what chi.Walk reports, or the assertion
// would pass by accident on every route.
func TestRecordedKeysMatchChiPatterns(t *testing.T) {
	s := testServer()
	r := chi.NewRouter()

	routes{server: s, router: r}.group("/api", func(api routes) {
		api.open(http.MethodGet, "/health", noop)
		api.group("/tokens", func(tk routes) {
			tk.guarded(http.MethodGet, "/", auth.PermSelf, noop)
			tk.guarded(http.MethodDelete, "/{tokenID}", auth.PermSelf, noop)
		})
	})

	walked := map[string]bool{}
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		walked[routeKey(method, route)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}

	for key := range s.policies {
		if !walked[key] {
			t.Errorf("recorded policy %q matches no route chi actually serves", key)
		}
	}
	for key := range walked {
		if _, ok := s.policies[key]; !ok {
			t.Errorf("route %q is served but has no recorded policy", key)
		}
	}
}

// The real router is the thing that ships; a missing policy there must fail
// the build, not just a synthetic fixture.
func TestProductionRoutesAreFullyGuarded(t *testing.T) {
	s := &Server{Log: slog.New(slog.DiscardHandler)}
	if _, err := s.Routes(); err != nil {
		t.Fatalf("Routes: %v", err)
	}

	var guarded, public int
	for key, p := range s.policies {
		if p.public {
			public++
			continue
		}
		guarded++
		if p.permission == "" {
			t.Errorf("route %q is guarded but declares no permission", key)
		}
	}

	if guarded == 0 {
		t.Fatal("no guarded routes registered")
	}

	// Public routes are the ones an unauthenticated visitor can reach, so the
	// set is pinned. Adding one should be a deliberate, reviewed change.
	wantPublic := map[string]bool{
		"GET /api/health":                           true,
		"GET /api/auth/setup":                       true,
		"POST /api/auth/setup":                      true,
		"POST /api/auth/login":                      true,
		"POST /api/auth/login/2fa":                  true,
		"GET /api/auth/invitations/{token}":         true,
		"POST /api/auth/invitations/{token}/accept": true,
		"POST /api/deployments/{deploymentID}/webhook": true,
	}
	for key, p := range s.policies {
		if p.public && !wantPublic[key] {
			t.Errorf("route %q is public but is not in the reviewed allowlist", key)
		}
	}
	for key := range wantPublic {
		p, ok := s.policies[key]
		if !ok {
			t.Errorf("expected public route %q is not registered", key)
			continue
		}
		if !p.public {
			t.Errorf("route %q was expected to be public", key)
		}
	}
}
