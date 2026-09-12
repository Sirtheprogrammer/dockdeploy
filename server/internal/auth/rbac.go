package auth

import "fmt"

// Role is a user's global capability level. Per-resource grants (which servers
// a member may touch) layer on top of this and are checked separately.
type Role string

const (
	// RoleAdmin can do everything, including managing users and credentials.
	RoleAdmin Role = "admin"
	// RoleMember can operate servers and deployments they have been granted.
	RoleMember Role = "member"
	// RoleViewer can look but not touch.
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

func (r Role) String() string { return string(r) }

// ParseRole validates a role coming from a request body.
func ParseRole(s string) (Role, error) {
	role := Role(s)
	if !role.Valid() {
		return "", fmt.Errorf("role must be one of admin, member, viewer")
	}
	return role, nil
}

// Permission is the unit every guarded route declares. Routes name a
// permission, roles hold a set of them, and the middleware compares the two --
// so adding a role never means auditing handlers.
type Permission string

const (
	PermServerRead   Permission = "server:read"
	PermServerWrite  Permission = "server:write"
	PermServerDelete Permission = "server:delete"

	// PermContainerOperate covers start/stop/restart and exec: acting on
	// containers the platform did not create.
	PermContainerOperate Permission = "container:operate"

	PermDeploymentRead   Permission = "deployment:read"
	PermDeploymentWrite  Permission = "deployment:write"
	PermDeploymentDeploy Permission = "deployment:deploy"
	PermDeploymentDelete Permission = "deployment:delete"

	PermDomainRead  Permission = "domain:read"
	PermDomainWrite Permission = "domain:write"

	PermCredentialRead  Permission = "credential:read"
	PermCredentialWrite Permission = "credential:write"

	PermUserRead  Permission = "user:read"
	PermUserWrite Permission = "user:write"

	PermAuditRead Permission = "audit:read"

	// PermSelf is held by every signed-in user: their own profile, their own
	// API tokens, signing out.
	PermSelf Permission = "self"
)

// viewerPermissions is the read-only floor every role inherits.
var viewerPermissions = []Permission{
	PermSelf,
	PermServerRead,
	PermDeploymentRead,
	PermDomainRead,
}

// memberPermissions adds day-to-day operation. Members deliberately cannot
// manage credentials, users, or delete servers.
var memberPermissions = append([]Permission{
	PermServerWrite,
	PermContainerOperate,
	PermDeploymentWrite,
	PermDeploymentDeploy,
	PermDeploymentDelete,
	PermDomainWrite,
}, viewerPermissions...)

var rolePermissions = map[Role]map[Permission]bool{
	RoleViewer: toSet(viewerPermissions),
	RoleMember: toSet(memberPermissions),
	RoleAdmin: toSet(append([]Permission{
		PermServerDelete,
		PermCredentialRead,
		PermCredentialWrite,
		PermUserRead,
		PermUserWrite,
		PermAuditRead,
	}, memberPermissions...)),
}

func toSet(perms []Permission) map[Permission]bool {
	set := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		set[p] = true
	}
	return set
}

// Can reports whether the role grants the permission.
func (r Role) Can(p Permission) bool {
	return rolePermissions[r][p]
}

// Permissions lists everything the role grants, for the client to hide actions
// it would only be rejected for. The server still enforces every one of them.
func (r Role) Permissions() []Permission {
	set := rolePermissions[r]
	out := make([]Permission, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	return out
}
