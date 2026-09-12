package api

import (
	"testing"

	"github.com/go-chi/chi/v5"
)

func newRouteContext(params map[string]string, order []string) *chi.Context {
	rctx := chi.NewRouteContext()
	for _, key := range order {
		rctx.URLParams.Add(key, params[key])
	}
	return rctx
}

func TestInferResource(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		order    []string
		params   map[string]string
		wantType string
		wantID   string
	}{
		{
			name:     "collection route",
			pattern:  "/api/users",
			wantType: "users",
		},
		{
			name:     "identified resource",
			pattern:  "/api/users/{userID}",
			order:    []string{"userID"},
			params:   map[string]string{"userID": "abc-123"},
			wantType: "users",
			wantID:   "abc-123",
		},
		{
			name:     "nested resource uses the innermost id",
			pattern:  "/api/servers/{serverID}/containers/{containerID}",
			order:    []string{"serverID", "containerID"},
			params:   map[string]string{"serverID": "srv-1", "containerID": "ctr-9"},
			wantType: "servers",
			wantID:   "ctr-9",
		},
		{
			// The invitation token is a live credential and the audit log is
			// readable by every admin, so it must never be recorded.
			name:     "credential in the path is not recorded",
			pattern:  "/api/auth/invitations/{token}/accept",
			order:    []string{"token"},
			params:   map[string]string{"token": "s3cr3t-invitation-token"},
			wantType: "auth",
			wantID:   "",
		},
		{
			name:     "credential alongside an id records only the id",
			pattern:  "/api/things/{thingID}/{token}",
			order:    []string{"thingID", "token"},
			params:   map[string]string{"thingID": "thing-1", "token": "s3cr3t"},
			wantType: "things",
			wantID:   "thing-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotID := inferResource(newRouteContext(tc.params, tc.order), tc.pattern)
			if gotType != tc.wantType {
				t.Errorf("resourceType = %q, want %q", gotType, tc.wantType)
			}
			if gotID != tc.wantID {
				t.Errorf("resourceID = %q, want %q", gotID, tc.wantID)
			}
			for _, value := range tc.params {
				if gotID == value && value == "s3cr3t-invitation-token" {
					t.Error("a path credential leaked into the audit resource id")
				}
			}
		})
	}
}

func TestInferResourceWithoutRouteContext(t *testing.T) {
	gotType, gotID := inferResource(nil, "/api/servers")
	if gotType != "servers" || gotID != "" {
		t.Errorf("inferResource(nil) = (%q, %q), want (servers, )", gotType, gotID)
	}
}
