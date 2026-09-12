package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/gitx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,48}[a-z0-9]$`)

// slugify turns a display name into an identifier safe for a directory name,
// a container name and a compose project name.
func slugify(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	lastDash := false
	for _, r := range lowered {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash && builder.Len() > 0:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if len(slug) > 50 {
		slug = strings.Trim(slug[:50], "-")
	}
	return slug
}

type deploymentResponse struct {
	store.Deployment
	ServerName string `json:"server_name"`
}

// requireDeployment loads a deployment and checks the caller may reach the
// server it lives on. Access is inherited from the server rather than tracked
// separately: a deployment is only meaningful on the machine it runs on.
func (s *Server) requireDeployment(r *http.Request) (*store.Deployment, *store.Server, error) {
	id := chi.URLParam(r, "deploymentID")
	actor := MustIdentity(r.Context())

	deployment, err := s.Store.DeploymentByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, NotFound("No such deployment.")
		}
		return nil, nil, Internal(err)
	}

	allowed, err := s.Store.CanAccessServer(r.Context(), deployment.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return nil, nil, Internal(err)
	}
	if !allowed {
		return nil, nil, NotFound("No such deployment.")
	}

	server, err := s.Store.ServerByID(r.Context(), deployment.ServerID)
	if err != nil {
		return nil, nil, Internal(err)
	}
	return deployment, server, nil
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) error {
	actor := MustIdentity(r.Context())
	deployments, err := s.Store.ListDeployments(r.Context(), actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}

	// Server names are looked up once and joined in memory; the list is small
	// and this avoids a query per row.
	servers, err := s.Store.ListServers(r.Context(), actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	names := make(map[string]string, len(servers))
	for _, server := range servers {
		names[server.ID] = server.Name
	}

	out := make([]deploymentResponse, 0, len(deployments))
	for _, deployment := range deployments {
		out = append(out, deploymentResponse{Deployment: deployment, ServerName: names[deployment.ServerID]})
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"deployments": out})
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) error {
	deployment, server, err := s.requireDeployment(r)
	if err != nil {
		return err
	}
	return JSON(w, s.Log, http.StatusOK, deploymentResponse{
		Deployment: *deployment, ServerName: server.Name,
	})
}

type createDeploymentRequest struct {
	ServerID   string `json:"server_id"`
	Name       string `json:"name"`
	SourceType string `json:"source_type"`

	RepoURL         string  `json:"repo_url"`
	GitRef          string  `json:"git_ref"`
	GitCredentialID *string `json:"git_credential_id"`

	DockerfilePath string `json:"dockerfile_path"`
	BuildContext   string `json:"build_context"`
	ComposePath    string `json:"compose_path"`
	ComposeContent string `json:"compose_content"`
	ImageRef       string `json:"image_ref"`

	BuildStrategy string  `json:"build_strategy"`
	RegistryID    *string `json:"registry_id"`

	ContainerPort int `json:"container_port"`

	Env []store.EnvVar `json:"env"`
}

func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) error {
	var req createDeploymentRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	name := f.required("name", req.Name, 1, 100)

	source := store.SourceType(req.SourceType)
	if !source.Valid() {
		f.add("source_type", "Choose a valid source.")
	}

	strategy := store.BuildStrategy(req.BuildStrategy)
	if strategy == "" {
		strategy = store.BuildRemote
	}
	if !strategy.Valid() {
		f.add("build_strategy", "Choose either remote or registry.")
	}

	if strategy == store.BuildRegistry {
		if req.RegistryID == nil || *req.RegistryID == "" {
			f.add("registry_id", "A container registry is required for controller builds.")
		} else {
			if _, err := s.Store.RegistryByID(r.Context(), *req.RegistryID); err != nil {
				f.add("registry_id", "Choose a valid registry.")
			}
		}
		if !source.NeedsGit() {
			f.add("build_strategy", "Registry builds require a git repository source.")
		}
	}

	switch source {
	case store.SourceGitDockerfile, store.SourceGitCompose:
		if err := gitx.ValidateRepoURL(req.RepoURL); err != nil {
			f.add("repo_url", capitalise(err.Error()))
		}
		if err := gitx.ValidateRef(req.GitRef); err != nil {
			f.add("git_ref", capitalise(err.Error()))
		}
	case store.SourceRawCompose:
		if strings.TrimSpace(req.ComposeContent) == "" {
			f.add("compose_content", "Paste a compose file.")
		}
	case store.SourceImage:
		if strings.TrimSpace(req.ImageRef) == "" {
			f.add("image_ref", "Enter an image, for example nginx:alpine.")
		}
	}

	port := req.ContainerPort
	if port == 0 {
		port = 80
	}
	if port < 1 || port > 65535 {
		f.add("container_port", "Port must be between 1 and 65535.")
	}

	for _, v := range req.Env {
		if !envKeyPattern.MatchString(v.Key) {
			f.add("env", "Environment variable names must start with a letter or underscore.")
			break
		}
		if strings.ContainsAny(v.Value, "\n\r") {
			f.add("env", v.Key+" cannot contain a line break.")
			break
		}
	}

	if err := f.err(); err != nil {
		return err
	}

	// Membership check before anything is written.
	actor := MustIdentity(r.Context())
	allowed, err := s.Store.CanAccessServer(r.Context(), req.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return Invalid(fields{"server_id": "Choose a server you have access to."})
	}

	slug := slugify(name)
	if !slugPattern.MatchString(slug) {
		return Invalid(fields{"name": "Use a name with at least two letters or numbers."})
	}

	webhookSecret, _, err := s.Hasher.NewToken()
	if err != nil {
		return Internal(err)
	}

	created, err := s.Store.CreateDeployment(r.Context(), s.Sealer, store.NewDeployment{
		ServerID:        req.ServerID,
		Name:            name,
		Slug:            slug,
		SourceType:      source,
		RepoURL:         strings.TrimSpace(req.RepoURL),
		GitRef:          strings.TrimSpace(req.GitRef),
		GitCredentialID: req.GitCredentialID,
		DockerfilePath:  defaultTo(req.DockerfilePath, "Dockerfile"),
		BuildContext:    defaultTo(req.BuildContext, "."),
		ComposePath:     defaultTo(req.ComposePath, "docker-compose.yml"),
		ComposeContent:  req.ComposeContent,
		ImageRef:        strings.TrimSpace(req.ImageRef),
		BuildStrategy:   strategy,
		RegistryID:      req.RegistryID,
		Workdir:         ".dockdeploy/apps/" + slug,
		ContainerPort:   port,
		WebhookSecret:   webhookSecret,
		CreatedBy:       actor.User.ID,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Invalid(fields{"name": "A deployment with that name already exists on this server."})
		}
		return Internal(err)
	}

	if len(req.Env) > 0 {
		if err := s.Store.SetDeploymentEnv(r.Context(), s.Sealer, created.ID, req.Env); err != nil {
			return Internal(err)
		}
	}

	AuditResource(r.Context(), "deployments", created.ID)
	AuditMeta(r.Context(), "name", created.Name)
	AuditMeta(r.Context(), "server_id", created.ServerID)

	return JSON(w, s.Log, http.StatusCreated, created)
}

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func defaultTo(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (s *Server) handleDeleteDeployment(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	if err := s.Store.DeleteDeployment(r.Context(), deployment.ID); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "deployments", deployment.ID)
	AuditMeta(r.Context(), "name", deployment.Name)
	// Say plainly what was and was not touched: the record is gone but the
	// container is still running on the server.
	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"deleted": true,
		"note": "The deployment record was removed. Its container is still running on the server; " +
			"stop it from the server page if you no longer want it.",
	})
}

// --- environment ---------------------------------------------------------

func (s *Server) handleGetDeploymentEnv(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}
	env, err := s.Store.ListDeploymentEnv(r.Context(), deployment.ID)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"env": env})
}

type setEnvRequest struct {
	Env []store.EnvVar `json:"env"`
}

func (s *Server) handleSetDeploymentEnv(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	var req setEnvRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	seen := map[string]bool{}
	for _, v := range req.Env {
		if !envKeyPattern.MatchString(v.Key) {
			return Invalid(fields{"env": "Names must start with a letter or underscore and contain only letters, numbers and underscores."})
		}
		if seen[v.Key] {
			return Invalid(fields{"env": "Each name may only appear once: " + v.Key})
		}
		// Env files are line-based, so a newline in a value would silently
		// corrupt everything after it. Docker has no escape for this.
		if strings.ContainsAny(v.Value, "\n\r") {
			return Invalid(fields{"env": v.Key + " cannot contain a line break."})
		}
		seen[v.Key] = true
	}

	if err := s.Store.SetDeploymentEnv(r.Context(), s.Sealer, deployment.ID, req.Env); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "deployments", deployment.ID)
	// Names only. Values are the whole point of the encryption.
	names := make([]string, 0, len(req.Env))
	for _, v := range req.Env {
		names = append(names, v.Key)
	}
	AuditMeta(r.Context(), "variables", strings.Join(names, ","))

	return NoContent(w)
}

func (s *Server) handleGetDeploymentWebhook(w http.ResponseWriter, r *http.Request) error {
	deployment, _, err := s.requireDeployment(r)
	if err != nil {
		return err
	}

	secret, err := s.Store.DeploymentWebhookSecret(r.Context(), s.Sealer, deployment)
	if err != nil {
		return Internal(err)
	}

	appURL := strings.TrimRight(s.Config.AppURL, "/")
	webhookURL := fmt.Sprintf("%s/api/deployments/%s/webhook", appURL, deployment.ID)

	return JSON(w, s.Log, http.StatusOK, map[string]string{
		"webhook_url":    webhookURL,
		"webhook_secret": secret,
	})
}

// handleTriggerWebhook handles incoming push-to-deploy webhooks from GitHub, GitLab, or curl.
func (s *Server) handleTriggerWebhook(w http.ResponseWriter, r *http.Request) error {
	deploymentID := chi.URLParam(r, "deploymentID")
	deployment, err := s.Store.DeploymentByID(r.Context(), deploymentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("Deployment not found.")
		}
		return Internal(err)
	}

	secret, err := s.Store.DeploymentWebhookSecret(r.Context(), s.Sealer, deployment)
	if err != nil {
		return Internal(err)
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return BadRequest("Failed to read payload.")
	}

	// Verify authentication
	authenticated := false

	// 1. GitHub HMAC-SHA256 signature
	if ghSig := r.Header.Get("X-Hub-Signature-256"); ghSig != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(ghSig), []byte(expected)) {
			authenticated = true
		}
	}

	// 2. Direct token in headers
	if !authenticated {
		if token := r.Header.Get("X-Dockdeploy-Token"); token != "" && token == secret {
			authenticated = true
		} else if token := r.Header.Get("X-Gitlab-Token"); token != "" && token == secret {
			authenticated = true
		} else if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == secret {
			authenticated = true
		}
	}

	// 3. Query parameter ?token=
	if !authenticated {
		if token := r.URL.Query().Get("token"); token != "" && token == secret {
			authenticated = true
		}
	}

	if !authenticated {
		return Unauthorized("Invalid webhook signature or token.")
	}

	// Enqueue run triggered by webhook
	run, err := s.Store.EnqueueRun(r.Context(), deployment.ID, store.TriggerWebhook, nil)
	if err != nil {
		return Internal(err)
	}

	s.Log.Info("deploy queued via webhook",
		"deployment", deployment.Name, "run", run.Number)

	return JSON(w, s.Log, http.StatusAccepted, run)
}

