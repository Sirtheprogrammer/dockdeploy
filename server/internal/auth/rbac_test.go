package auth

import "testing"

// The matrix is the whole authorization model, so it is pinned explicitly
// rather than derived from the same tables the implementation uses. A change
// to a role's reach should have to be made twice: once in code, once here.
func TestRolePermissionMatrix(t *testing.T) {
	cases := []struct {
		perm  Permission
		admin bool
		membr bool
		viewr bool
	}{
		{PermSelf, true, true, true},

		{PermServerRead, true, true, true},
		{PermServerWrite, true, true, false},
		{PermServerDelete, true, false, false},
		{PermContainerOperate, true, true, false},

		{PermDeploymentRead, true, true, true},
		{PermDeploymentWrite, true, true, false},
		{PermDeploymentDeploy, true, true, false},
		{PermDeploymentDelete, true, true, false},

		{PermDomainRead, true, true, true},
		{PermDomainWrite, true, true, false},

		{PermCredentialRead, true, false, false},
		{PermCredentialWrite, true, false, false},

		{PermUserRead, true, false, false},
		{PermUserWrite, true, false, false},
		{PermAuditRead, true, false, false},
	}

	for _, tc := range cases {
		for role, want := range map[Role]bool{
			RoleAdmin:  tc.admin,
			RoleMember: tc.membr,
			RoleViewer: tc.viewr,
		} {
			if got := role.Can(tc.perm); got != want {
				t.Errorf("%s.Can(%s) = %v, want %v", role, tc.perm, got, want)
			}
		}
	}
}

func TestUnknownRoleGrantsNothing(t *testing.T) {
	// A role read from a corrupted row must deny rather than default to admin.
	rogue := Role("superuser")
	if rogue.Valid() {
		t.Fatal("unknown role reported as valid")
	}
	for _, perm := range []Permission{PermSelf, PermServerRead, PermUserWrite} {
		if rogue.Can(perm) {
			t.Errorf("unknown role granted %s", perm)
		}
	}
	if got := len(rogue.Permissions()); got != 0 {
		t.Errorf("unknown role has %d permissions, want 0", got)
	}
}

func TestRoleHierarchyIsInclusive(t *testing.T) {
	// Every viewer permission must be held by members, and every member
	// permission by admins. A gap here means a promotion loses access.
	for _, perm := range RoleViewer.Permissions() {
		if !RoleMember.Can(perm) {
			t.Errorf("member is missing viewer permission %s", perm)
		}
	}
	for _, perm := range RoleMember.Permissions() {
		if !RoleAdmin.Can(perm) {
			t.Errorf("admin is missing member permission %s", perm)
		}
	}
}

func TestParseRole(t *testing.T) {
	for _, valid := range []string{"admin", "member", "viewer"} {
		if _, err := ParseRole(valid); err != nil {
			t.Errorf("ParseRole(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"", "Admin", "owner", "root", "ADMIN"} {
		if _, err := ParseRole(invalid); err == nil {
			t.Errorf("ParseRole(%q) = nil error, want rejection", invalid)
		}
	}
}
