package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/dockerx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// dockerTimeout bounds ordinary Engine calls. Streaming endpoints (logs,
// stats, exec) deliberately do not use it.
const dockerTimeout = 30 * time.Second

// session opens a Docker client for the server named in the URL, after
// checking the caller may reach it.
func (s *Server) session(r *http.Request) (*servers.Session, *store.Server, error) {
	server, err := s.requireServer(r)
	if err != nil {
		return nil, nil, err
	}

	session, err := s.Servers.Session(r.Context(), server)
	if err != nil {
		// A server that will not connect is an expected state, not a bug in
		// the request. 502 says the failure is beyond this service, and the
		// message is the actionable part.
		return nil, nil, Unavailable("%s", connectionMessage(err)).WithCause(err)
	}
	return session, server, nil
}

// connectionMessage turns a transport failure into something a user can act
// on. The manager already produces these; this keeps the wording in one place.
func connectionMessage(err error) string {
	switch {
	case errors.Is(err, dockerx.ErrSocketDenied):
		return "Connected over SSH, but this user cannot read the Docker socket. " +
			"Add the user to the docker group on that server, then try again."
	case errors.Is(err, dockerx.ErrDaemonUnreachable):
		return "Connected over SSH, but the Docker daemon did not respond."
	default:
		return "Could not connect to this server."
	}
}

func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) error {
	session, _, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dockerTimeout)
	defer cancel()

	containers, err := session.Docker.ListContainers(ctx)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"containers": containers})
}

func (s *Server) handleInspectContainer(w http.ResponseWriter, r *http.Request) error {
	session, _, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dockerTimeout)
	defer cancel()

	details, err := session.Docker.InspectContainer(ctx, chi.URLParam(r, "containerID"))
	if err != nil {
		return NotFound("No such container on this server.").WithCause(err)
	}
	return JSON(w, s.Log, http.StatusOK, details)
}

type containerActionRequest struct {
	Action string `json:"action"`
}

// handleContainerAction performs a lifecycle operation.
//
// This works on containers the platform did not create, which is the whole
// point of discovering what is already running on a machine. Every call is
// audited with the container name, because stopping something is destructive
// and "who stopped postgres" needs an answer.
func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) error {
	var req containerActionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	action := dockerx.Action(req.Action)
	if !action.Valid() {
		return Invalid(fields{"action": "Unknown container action."})
	}

	session, server, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	containerID := chi.URLParam(r, "containerID")

	// Record the name before acting: after a remove, it is gone.
	name := containerID
	if details, err := session.Docker.InspectContainer(ctx, containerID); err == nil {
		name = details.Name
	}

	if err := session.Docker.Do(ctx, action, containerID); err != nil {
		return Conflict("Docker refused to %s this container.", action).WithCause(err)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "action", string(action))
	AuditMeta(r.Context(), "container", name)
	s.Log.Info("container action",
		"server", server.Name, "container", name, "action", action,
		"actor", MustIdentity(r.Context()).User.Email)

	return NoContent(w)
}

func (s *Server) handleListImages(w http.ResponseWriter, r *http.Request) error {
	session, _, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dockerTimeout)
	defer cancel()

	images, err := session.Docker.ListImages(ctx)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"images": images})
}

func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) error {
	session, _, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dockerTimeout)
	defer cancel()

	volumes, err := session.Docker.ListVolumes(ctx)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"volumes": volumes})
}

func (s *Server) handleListNetworks(w http.ResponseWriter, r *http.Request) error {
	session, _, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dockerTimeout)
	defer cancel()

	networks, err := session.Docker.ListNetworks(ctx)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"networks": networks})
}

func (s *Server) handleDockerInfo(w http.ResponseWriter, r *http.Request) error {
	session, _, err := s.session(r)
	if err != nil {
		return err
	}
	defer session.Close()

	ctx, cancel := context.WithTimeout(r.Context(), dockerTimeout)
	defer cancel()

	info, err := session.Docker.Info(ctx)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, info)
}
