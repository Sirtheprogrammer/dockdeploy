package mcp

import (
	"context"
	"fmt"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
)

// Caller is the authenticated identity behind an MCP request arriving over
// HTTP. The stdio transport has no caller: whoever can launch `dockdeploy mcp`
// already holds the database and encryption key, so it runs with full access.
type Caller struct {
	UserID string
	Role   auth.Role
}

func (c *Caller) isAdmin() bool { return c.Role == auth.RoleAdmin }

type callerKey struct{}

// WithCaller attaches the HTTP caller so tool calls are authorized against the
// same role permissions and server grants as the REST API.
func WithCaller(ctx context.Context, c *Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

func callerFrom(ctx context.Context) (*Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(*Caller)
	return c, ok && c != nil
}

// toolPermissions mirrors the permission the equivalent REST route requires,
// so an MCP token can never do more than the same user could in the dashboard.
var toolPermissions = map[string]auth.Permission{
	"dockdeploy_list_servers":        auth.PermServerRead,
	"dockdeploy_get_server":          auth.PermServerRead,
	"dockdeploy_exec_command":        auth.PermServerWrite,
	"dockdeploy_docker_exec":         auth.PermContainerOperate,
	"dockdeploy_list_containers":     auth.PermServerRead,
	"dockdeploy_inspect_container":   auth.PermServerRead,
	"dockdeploy_container_action":    auth.PermContainerOperate,
	"dockdeploy_get_container_logs":  auth.PermServerRead,
	"dockdeploy_restart_container":   auth.PermContainerOperate,
	"dockdeploy_read_file":           auth.PermServerRead,
	"dockdeploy_write_file":          auth.PermServerWrite,
	"dockdeploy_list_files":          auth.PermServerRead,
	"dockdeploy_list_deployments":    auth.PermDeploymentRead,
	"dockdeploy_trigger_deployment":  auth.PermDeploymentDeploy,
	"dockdeploy_get_deployment_logs": auth.PermDeploymentRead,
	"dockdeploy_server_diagnostics":  auth.PermServerRead,
	"dockdeploy_security_audit":      auth.PermServerRead,
}

// toolAllowed reports whether the caller in ctx may invoke the tool. Unknown
// tools fail closed.
func toolAllowed(ctx context.Context, name string) bool {
	perm, known := toolPermissions[name]
	if !known {
		return false
	}
	c, ok := callerFrom(ctx)
	if !ok {
		return true
	}
	return c.Role.Can(perm)
}

// listScope returns the (userID, all) pair the store's List* methods take.
func listScope(ctx context.Context) (string, bool) {
	c, ok := callerFrom(ctx)
	if !ok {
		return "", true
	}
	return c.UserID, c.isAdmin()
}

// checkServerAccess applies per-server grants for non-admin HTTP callers.
func (s *Server) checkServerAccess(ctx context.Context, serverID string) error {
	c, ok := callerFrom(ctx)
	if !ok || c.isAdmin() {
		return nil
	}
	allowed, err := s.Store.CanAccessServer(ctx, serverID, c.UserID, false)
	if err != nil {
		return err
	}
	if !allowed {
		// Same wording as a missing server, like the REST API.
		return fmt.Errorf("server %q not found", serverID)
	}
	return nil
}

// canCreateServers reports whether resolving a server may auto-register the
// local Docker host. A read-only call must not create records.
func canCreateServers(ctx context.Context) bool {
	c, ok := callerFrom(ctx)
	if !ok {
		return true
	}
	return c.Role.Can(auth.PermServerWrite)
}
