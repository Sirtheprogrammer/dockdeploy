package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// API tokens exist so a CI pipeline can trigger a deploy without a browser
// session. They inherit the role of the user who created them, so a viewer
// cannot mint a token that deploys.

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	tokens, err := s.Store.ListAPITokens(r.Context(), id.User.ID)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"tokens": tokens})
}

type createTokenRequest struct {
	Name string `json:"name"`
	// ExpiresInDays of 0 means the token never expires, which is the usual
	// choice for a long-lived CI credential.
	ExpiresInDays int `json:"expires_in_days"`
}

type createTokenResponse struct {
	store.APIToken
	// Token is shown once and never retrievable again: only its hash is kept.
	Token string `json:"token"`
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) error {
	var req createTokenRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	if req.ExpiresInDays < 0 || req.ExpiresInDays > 3650 {
		f.add("expires_in_days", "Expiry must be between 0 and 3650 days.")
	}
	if err := f.err(); err != nil {
		return err
	}

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		at := time.Now().AddDate(0, 0, req.ExpiresInDays)
		expiresAt = &at
	}

	token, hash, prefix, err := s.Hasher.NewAPIToken()
	if err != nil {
		return Internal(err)
	}

	id := MustIdentity(r.Context())
	created, err := s.Store.CreateAPIToken(r.Context(), id.User.ID, name, hash, prefix, expiresAt)
	if err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "tokens", created.ID)
	AuditMeta(r.Context(), "name", name)

	return JSON(w, s.Log, http.StatusCreated, createTokenResponse{APIToken: *created, Token: token})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) error {
	id := MustIdentity(r.Context())
	tokenID := chi.URLParam(r, "tokenID")

	if err := s.Store.DeleteAPIToken(r.Context(), tokenID, id.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such token.")
		}
		return Internal(err)
	}
	AuditResource(r.Context(), "tokens", tokenID)
	return NoContent(w)
}

// --- audit log ----------------------------------------------------------

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query()

	filter := store.AuditFilter{
		UserID:       query.Get("user_id"),
		ResourceType: query.Get("resource_type"),
		ResourceID:   query.Get("resource_id"),
		Limit:        atoiDefault(query.Get("limit"), 50),
	}
	if before := query.Get("before"); before != "" {
		at, err := time.Parse(time.RFC3339, before)
		if err != nil {
			return BadRequest("The 'before' parameter must be an RFC 3339 timestamp.")
		}
		filter.Before = &at
	}

	entries, err := s.Store.ListAudit(r.Context(), filter)
	if err != nil {
		return Internal(err)
	}

	// The client pages by passing the oldest timestamp it has seen back as
	// 'before', which stays stable while new entries arrive at the top.
	var next *time.Time
	if len(entries) == filter.Limit && filter.Limit > 0 {
		next = &entries[len(entries)-1].CreatedAt
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"entries": entries,
		"before":  next,
	})
}

// atoiDefault parses a bounded page size, falling back rather than failing the
// request on a malformed query parameter.
func atoiDefault(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 200 {
		return fallback
	}
	return n
}
