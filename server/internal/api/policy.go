package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
)

// The authorization model is declarative: a route names the permission it
// needs at the point it is registered, and assertPolicies refuses to start the
// server if any route was registered without one.
//
// The alternative -- checking permissions inside each handler -- fails open:
// a handler that forgets the check simply works, and nothing detects it.

// routes registers handlers while recording the permission each one requires.
type routes struct {
	server *Server
	router chi.Router
	prefix string
}

// group returns a nested registrar for a subtree.
func (rt routes) group(prefix string, build func(routes)) {
	rt.router.Route(prefix, func(r chi.Router) {
		build(routes{server: rt.server, router: r, prefix: rt.prefix + prefix})
	})
}

// guarded registers a route that requires an authenticated caller holding perm.
func (rt routes) guarded(method, pattern string, perm auth.Permission, h Handler) {
	rt.record(method, pattern, perm, false)
	rt.router.Method(method, pattern, rt.server.wrap(rt.server.requirePermission(perm, h)))
}

// open registers a route reachable without authentication. Every use is a
// deliberate decision: sign-in, first-run setup, accepting an invitation, and
// health.
func (rt routes) open(method, pattern string, h Handler) {
	rt.record(method, pattern, "", true)
	rt.router.Method(method, pattern, rt.server.wrap(h))
}

func (rt routes) record(method, pattern string, perm auth.Permission, public bool) {
	key := routeKey(method, joinPattern(rt.prefix, pattern))
	if _, exists := rt.server.policies[key]; exists {
		panic("api: duplicate route registration: " + key)
	}
	rt.server.policies[key] = policy{permission: perm, public: public}
}

type policy struct {
	permission auth.Permission
	public     bool
}

func routeKey(method, pattern string) string { return method + " " + pattern }

// joinPattern mirrors how chi composes a mounted prefix with a leaf pattern,
// so recorded keys match what chi.Walk later reports.
func joinPattern(prefix, pattern string) string {
	switch {
	case pattern == "/":
		if prefix == "" {
			return "/"
		}
		return prefix + "/"
	case prefix == "":
		return pattern
	default:
		return prefix + pattern
	}
}

// assertPolicies fails startup if any registered route lacks a policy.
//
// This is the backstop for the whole authorization model: adding a route with
// a bare chi call rather than the helpers above is caught here instead of
// shipping as an unguarded endpoint.
func assertPolicies(router chi.Router, policies map[string]policy) error {
	var unguarded []string

	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// Only the API is access-controlled; the SPA is public static assets.
		if !strings.HasPrefix(route, "/api") {
			return nil
		}
		if _, ok := policies[routeKey(method, route)]; !ok {
			unguarded = append(unguarded, routeKey(method, route))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("api: walk routes: %w", err)
	}

	if len(unguarded) > 0 {
		sort.Strings(unguarded)
		return fmt.Errorf(
			"api: %d route(s) registered without an access policy; use routes.guarded or routes.open:\n  - %s",
			len(unguarded), strings.Join(unguarded, "\n  - "))
	}
	return nil
}
