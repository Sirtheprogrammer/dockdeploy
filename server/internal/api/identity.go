package api

import (
	"context"
	"net"
	"net/http"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

const identityKey contextKey = "identity"

// Identity is the authenticated caller for one request.
type Identity struct {
	User store.User
	// SessionID is set for browser sessions, TokenID for API tokens. Exactly
	// one of them is non-empty.
	SessionID string
	TokenID   string
}

func (i *Identity) Role() auth.Role { return i.User.Role }

func (i *Identity) Can(p auth.Permission) bool { return i.User.Role.Can(p) }

// ViaToken reports whether the caller authenticated with an API token rather
// than a browser session.
func (i *Identity) ViaToken() bool { return i.TokenID != "" }

func withIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// IdentityFrom returns the authenticated caller, if any.
func IdentityFrom(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityKey).(*Identity)
	return id, ok && id != nil
}

// MustIdentity returns the caller on a guarded route, where the authorize step
// has already established that one exists.
func MustIdentity(ctx context.Context) *Identity {
	id, ok := IdentityFrom(ctx)
	if !ok {
		panic("api: guarded handler reached without an identity")
	}
	return id
}

// clientIP strips the port from RemoteAddr. RealIP has already applied any
// trusted proxy headers by this point.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
