package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.Store.ListRuns(r.Context(), deployment.ID, limit)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"runs": runs})
}

type deployRequest struct {
	// Rollback names a previous successful run whose image should be restored.
	Rollback string `json:"rollback_run_id"`
}

// handleDeploy queues a run.
//
// It returns as soon as the run is queued rather than waiting for the build.
// A deploy takes minutes, and holding the request open would mean a refresh
// orphans the build and a proxy timeout looks like a failure.
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	var req deployRequest
	if r.ContentLength > 0 {
		if err := DecodeJSON(w, r, &req); err != nil {
			return err
		}
	}

	actor := MustIdentity(r.Context())
	trigger := store.TriggerManual
	if actor.ViaToken() {
		trigger = store.TriggerAPI
	}

	var run *store.Run
	if req.Rollback != "" {
		targetRun, err := s.Store.RunByID(r.Context(), req.Rollback)
		if err != nil || targetRun.DeploymentID != deployment.ID {
			return NotFound("No such run to roll back to.")
		}
		if targetRun.Status != store.RunSucceeded {
			return Invalid(fields{"rollback_run_id": "Can only roll back to a succeeded run."})
		}
		if targetRun.ImageRef == "" {
			return Invalid(fields{"rollback_run_id": "Selected run does not have a saved image."})
		}
		trigger = store.TriggerRollback
		run, err = s.Store.EnqueueRunWithParams(r.Context(), store.EnqueueRunParams{
			DeploymentID: deployment.ID,
			Trigger:      trigger,
			TriggeredBy:  &actor.User.ID,
			CommitSHA:    targetRun.CommitSHA,
			ImageRef:     targetRun.ImageRef,
		})
		if err != nil {
			return Internal(err)
		}
	} else {
		var err error
		run, err = s.Store.EnqueueRun(r.Context(), deployment.ID, trigger, &actor.User.ID)
		if err != nil {
			return Internal(err)
		}
	}

	AuditResource(r.Context(), "deployments", deployment.ID)
	AuditMeta(r.Context(), "run", run.Number)
	AuditMeta(r.Context(), "trigger", string(trigger))

	s.Log.Info("deploy queued",
		"deployment", deployment.Name, "run", run.Number, "actor", actor.User.Email)

	return JSON(w, s.Log, http.StatusAccepted, run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	runID := chi.URLParam(r, "runID")
	run, err := s.Store.RunByID(r.Context(), runID)
	if err != nil || run.DeploymentID != deployment.ID {
		return NotFound("No such run.")
	}

	if err := s.Store.CancelRun(r.Context(), runID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Only a queued run can be cancelled. Stopping a build midway
			// would leave the server in a state nobody can reason about.
			return Conflict("This run has already started, so it cannot be cancelled.")
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "deployments", deployment.ID)
	AuditMeta(r.Context(), "run", run.Number)
	return NoContent(w)
}

// handleRunLogs streams a run's output.
//
// Replay comes from the database and live output from the hub, in that order,
// so a client that opens the page mid-build sees the whole log rather than
// only what happens from the moment it connected.
func (s *Server) handleRunLogs(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return Internal(fmt.Errorf("api: response writer does not support flushing"))
	}

	runID := chi.URLParam(r, "runID")
	run, err := s.Store.RunByID(r.Context(), runID)
	if err != nil || run.DeploymentID != deployment.ID {
		return NotFound("No such run.")
	}

	// Attach before replaying. Subscribing first means a line written during
	// the replay is buffered rather than lost in the gap between the two.
	watcher, detach := s.Deploys.Hub().Watch(runID)
	defer detach()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writer := &eventWriter{w: w, flusher: flusher}

	var lastSeq int64 = -1
	for {
		entries, err := s.Store.ListLogs(r.Context(), runID, lastSeq, 2000)
		if err != nil {
			s.Log.Warn("replay run logs", "run", runID, "error", err)
			break
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			writer.event("log", entry)
			lastSeq = entry.Seq
		}
	}

	// A finished run has nothing more to say.
	if run.Status.Terminal() {
		writer.event("status", map[string]any{"status": run.Status, "error": run.Error})
		writer.event("end", map[string]string{"reason": "finished"})
		return nil
	}

	writer.event("replayed", map[string]any{"through": lastSeq})

	// Keeps intermediaries from closing an idle connection during a long
	// build step that produces no output.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	// Polls the run row so the stream ends when the build does, even if the
	// last log line arrived some time earlier.
	statusCheck := time.NewTicker(2 * time.Second)
	defer statusCheck.Stop()

	for {
		select {
		case <-r.Context().Done():
			return nil

		case entry, ok := <-watcher:
			if !ok {
				return nil
			}
			// Skip anything already covered by the replay.
			if entry.Seq <= lastSeq {
				continue
			}
			lastSeq = entry.Seq
			writer.event("log", entry)

		case <-heartbeat.C:
			writer.comment("keepalive")

		case <-statusCheck.C:
			current, err := s.Store.RunByID(r.Context(), runID)
			if err != nil || !current.Status.Terminal() {
				continue
			}
			// Drain anything the recorder wrote just before finishing.
			if entries, err := s.Store.ListLogs(r.Context(), runID, lastSeq, 2000); err == nil {
				for _, entry := range entries {
					writer.event("log", entry)
					lastSeq = entry.Seq
				}
			}
			writer.event("status", map[string]any{"status": current.Status, "error": current.Error})
			writer.event("end", map[string]string{"reason": "finished"})
			return nil
		}
	}
}

// comment sends an SSE comment frame, which clients ignore but which keeps the
// connection from being reaped as idle.
func (e *eventWriter) comment(text string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = fmt.Fprintf(e.w, ": %s\n\n", text)
	e.flusher.Flush()
}

// --- registries and git credentials --------------------------------------

func (s *Server) handleListRegistries(w http.ResponseWriter, r *http.Request) error {
	registries, err := s.Store.ListRegistries(r.Context())
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"registries": registries})
}

type createRegistryRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleCreateRegistry(w http.ResponseWriter, r *http.Request) error {
	var req createRegistryRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	url := f.required("url", req.URL, 1, 255)
	username := f.required("username", req.Username, 1, 100)
	if req.Password == "" {
		f.add("password", "A password or access token is required.")
	}
	if err := f.err(); err != nil {
		return err
	}

	actor := MustIdentity(r.Context())
	created, err := s.Store.CreateRegistry(r.Context(), s.Sealer, name, url, username, req.Password, actor.User.ID)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Invalid(fields{"name": "A registry with that name already exists."})
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "registries", created.ID)
	AuditMeta(r.Context(), "url", created.URL)
	return JSON(w, s.Log, http.StatusCreated, created)
}

func (s *Server) handleDeleteRegistry(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "registryID")
	if err := s.Store.DeleteRegistry(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such registry.")
		}
		return Internal(err)
	}
	AuditResource(r.Context(), "registries", id)
	return NoContent(w)
}

func (s *Server) handleListGitCredentials(w http.ResponseWriter, r *http.Request) error {
	credentials, err := s.Store.ListGitCredentials(r.Context())
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"credentials": credentials})
}

type createGitCredentialRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

func (s *Server) handleCreateGitCredential(w http.ResponseWriter, r *http.Request) error {
	var req createGitCredentialRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	kind := gitKind(req.Kind)
	if !kind.Valid() {
		f.add("kind", "Choose either a token or an SSH deploy key.")
	}
	if req.Secret == "" {
		f.add("secret", "Paste the token or private key.")
	}
	if err := f.err(); err != nil {
		return err
	}

	actor := MustIdentity(r.Context())
	created, err := s.Store.CreateGitCredential(r.Context(), s.Sealer, name, kind, req.Username, req.Secret, actor.User.ID)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Invalid(fields{"name": "A credential with that name already exists."})
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "git_credentials", created.ID)
	return JSON(w, s.Log, http.StatusCreated, created)
}

func (s *Server) handleDeleteGitCredential(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "credentialID")
	if err := s.Store.DeleteGitCredential(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such credential.")
		}
		return Internal(err)
	}
	AuditResource(r.Context(), "git_credentials", id)
	return NoContent(w)
}

// gitKind narrows a request string to a credential kind.
func gitKind(raw string) gitx.Kind { return gitx.Kind(raw) }
