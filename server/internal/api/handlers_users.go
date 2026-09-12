package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"users": users})
}

type updateUserRequest struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// handleUpdateUser changes another account.
//
// Two guard rails matter here: an admin must not be able to demote or suspend
// themselves by accident, and the instance must never end up with no active
// administrator able to fix things.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) error {
	var req updateUserRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	targetID := chi.URLParam(r, "userID")
	actor := MustIdentity(r.Context())

	target, err := s.Store.UserByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such user.")
		}
		return Internal(err)
	}

	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	role, err := auth.ParseRole(req.Role)
	if err != nil {
		f.add("role", err.Error())
	}
	status := store.UserStatus(req.Status)
	if status != store.UserActive && status != store.UserSuspended {
		f.add("status", "Status must be active or suspended.")
	}
	if err := f.err(); err != nil {
		return err
	}

	if target.ID == actor.User.ID {
		if role != target.Role {
			return Forbidden("You cannot change your own role. Ask another admin.")
		}
		if status != target.Status {
			return Forbidden("You cannot suspend your own account.")
		}
	}

	losingAnAdmin := target.Role == auth.RoleAdmin && target.Status == store.UserActive &&
		(role != auth.RoleAdmin || status != store.UserActive)
	if losingAnAdmin {
		admins, err := s.Store.CountAdmins(r.Context())
		if err != nil {
			return Internal(err)
		}
		if admins <= 1 {
			return Conflict("This is the only active admin. Promote someone else first.")
		}
	}

	updated, err := s.Store.UpdateUser(r.Context(), targetID, name, role, status)
	if err != nil {
		return Internal(err)
	}

	// A suspended account keeps working until its cookie is gone, so drop the
	// sessions rather than waiting for them to expire.
	if status == store.UserSuspended {
		if err := s.Store.DeleteSessionsForUser(r.Context(), targetID); err != nil {
			s.Log.Warn("drop sessions for suspended user", "user", targetID, "error", err)
		}
	}

	AuditResource(r.Context(), "users", targetID)
	AuditMeta(r.Context(), "role", string(role))
	AuditMeta(r.Context(), "status", string(status))
	return JSON(w, s.Log, http.StatusOK, updated)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) error {
	targetID := chi.URLParam(r, "userID")
	actor := MustIdentity(r.Context())

	if targetID == actor.User.ID {
		return Forbidden("You cannot delete your own account.")
	}

	target, err := s.Store.UserByID(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such user.")
		}
		return Internal(err)
	}

	if target.Role == auth.RoleAdmin && target.Status == store.UserActive {
		admins, err := s.Store.CountAdmins(r.Context())
		if err != nil {
			return Internal(err)
		}
		if admins <= 1 {
			return Conflict("This is the only active admin. Promote someone else first.")
		}
	}

	if err := s.Store.DeleteUser(r.Context(), targetID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such user.")
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "users", targetID)
	AuditMeta(r.Context(), "email", target.Email)
	return NoContent(w)
}

// --- invitations --------------------------------------------------------

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) error {
	invitations, err := s.Store.ListInvitations(r.Context())
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"invitations": invitations})
}

type createInvitationRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

type createInvitationResponse struct {
	store.Invitation
	// URL is returned exactly once, at creation. There is no mail server in a
	// self-hosted install, so the admin copies this link and sends it however
	// they like.
	URL string `json:"url"`
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) error {
	var req createInvitationRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	email := f.email("email", req.Email)
	role, err := auth.ParseRole(req.Role)
	if err != nil {
		f.add("role", err.Error())
	}
	if err := f.err(); err != nil {
		return err
	}

	if _, err := s.Store.UserByEmail(r.Context(), email); err == nil {
		return Invalid(fields{"email": "Someone with that email already has an account."})
	} else if !errors.Is(err, store.ErrNotFound) {
		return Internal(err)
	}

	token, hash, err := s.Hasher.NewToken()
	if err != nil {
		return Internal(err)
	}

	actor := MustIdentity(r.Context())
	invitation, err := s.Store.CreateInvitation(r.Context(), email, req.Name, role,
		actor.User.ID, hash, time.Now().Add(invitationLifetime))
	if err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "invitations", invitation.ID)
	AuditMeta(r.Context(), "email", email)
	AuditMeta(r.Context(), "role", string(role))

	return JSON(w, s.Log, http.StatusCreated, createInvitationResponse{
		Invitation: *invitation,
		URL:        s.Config.AppURL + "/invite/" + token,
	})
}

func (s *Server) handleDeleteInvitation(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "invitationID")
	if err := s.Store.DeleteInvitation(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such invitation.")
		}
		return Internal(err)
	}
	AuditResource(r.Context(), "invitations", id)
	return NoContent(w)
}
