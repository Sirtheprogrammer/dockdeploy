package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

const auditKey contextKey = "audit"

// auditContext lets a handler enrich the entry the middleware will write.
// Handlers that add nothing still produce a usable record.
type auditContext struct {
	mu           sync.Mutex
	resourceType string
	resourceID   string
	meta         map[string]any
}

// AuditResource records which object a request acted on, for handlers where it
// is not simply the last URL parameter.
func AuditResource(ctx context.Context, resourceType, resourceID string) {
	if ac, ok := ctx.Value(auditKey).(*auditContext); ok {
		ac.mu.Lock()
		defer ac.mu.Unlock()
		ac.resourceType, ac.resourceID = resourceType, resourceID
	}
}

// AuditMeta attaches a detail to the audit entry. Never pass a secret here:
// entries are readable by every admin.
func AuditMeta(ctx context.Context, key string, value any) {
	if ac, ok := ctx.Value(auditKey).(*auditContext); ok {
		ac.mu.Lock()
		defer ac.mu.Unlock()
		if ac.meta == nil {
			ac.meta = map[string]any{}
		}
		ac.meta[key] = value
	}
}

// auditWrites records every mutating API request, successful or not.
//
// Failures are as interesting as successes here: a burst of 403s is how a
// misconfigured role or a probing token shows up.
func (s *Server) auditWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}

		ac := &auditContext{}
		r = r.WithContext(context.WithValue(r.Context(), auditKey, ac))

		ww, ok := w.(middleware.WrapResponseWriter)
		if !ok {
			ww = middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			w = ww
		}

		next.ServeHTTP(w, r)

		// Read the route pattern only after routing has completed; before
		// that chi has not yet descended into the matched subtree.
		rctx := chi.RouteContext(r.Context())
		pattern := r.URL.Path
		if rctx != nil && rctx.RoutePattern() != "" {
			pattern = rctx.RoutePattern()
		}

		entry := store.AuditEntry{
			Action: r.Method + " " + pattern,
			Status: ww.Status(),
			IP:     clientIP(r),
		}
		if id, ok := IdentityFrom(r.Context()); ok {
			entry.UserID = &id.User.ID
			entry.ActorEmail = id.User.Email
		}

		ac.mu.Lock()
		entry.ResourceType, entry.ResourceID = ac.resourceType, ac.resourceID
		meta := ac.meta
		ac.mu.Unlock()

		if entry.ResourceType == "" {
			entry.ResourceType, entry.ResourceID = inferResource(rctx, pattern)
		}
		if len(meta) > 0 {
			if encoded, err := json.Marshal(meta); err == nil {
				entry.Meta = encoded
			}
		}

		// The request is already answered; a logging failure must not turn a
		// successful action into an error the user sees.
		if err := s.Store.WriteAudit(r.Context(), entry); err != nil {
			LoggerFrom(r.Context()).Error("write audit entry", "action", entry.Action, "error", err)
		}
	})
}

// inferResource derives a resource from the route: the first path segment
// after /api names the type, and an opaque identifier parameter names the
// object.
//
// Only parameters ending in "ID" are used. Some routes carry a credential in
// the path -- {token} on the invitation endpoints -- and the audit log is
// readable by every admin, so anything that is not an opaque row id stays out
// of it.
func inferResource(rctx *chi.Context, pattern string) (resourceType, resourceID string) {
	for _, segment := range strings.Split(strings.TrimPrefix(pattern, "/api/"), "/") {
		if segment != "" && !strings.HasPrefix(segment, "{") {
			resourceType = segment
			break
		}
	}

	if rctx != nil {
		for i := len(rctx.URLParams.Keys) - 1; i >= 0; i-- {
			if !strings.HasSuffix(rctx.URLParams.Keys[i], "ID") {
				continue
			}
			if i < len(rctx.URLParams.Values) {
				resourceID = rctx.URLParams.Values[i]
			}
			break
		}
	}
	return resourceType, resourceID
}
