package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/sshx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// --- adding a server ----------------------------------------------------

type fingerprintRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type fingerprintResponse struct {
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"key_type"`
}

// handleServerFingerprint reads a server's host key so the user can confirm it
// before any credential is sent.
//
// This is deliberately a separate step from creating the server. The SSH key
// exchange completes before authentication, so the identity of the machine can
// be shown and approved while the password or private key is still sitting in
// the browser. Collapsing the two steps would mean sending a credential to a
// host nobody has vouched for yet.
func (s *Server) handleServerFingerprint(w http.ResponseWriter, r *http.Request) error {
	var req fingerprintRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	host := f.required("host", req.Host, 1, 255)
	if req.Port < 0 || req.Port > 65535 {
		f.add("port", "Port must be between 1 and 65535.")
	}
	if err := f.err(); err != nil {
		return err
	}
	if req.Port == 0 {
		req.Port = 22
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	fingerprint, keyType, err := sshx.Fingerprint(ctx, host, req.Port)
	if err != nil {
		return BadRequest("Could not reach %s on port %d over SSH.", host, req.Port).WithCause(err)
	}

	AuditMeta(r.Context(), "host", host)
	return JSON(w, s.Log, http.StatusOK, fingerprintResponse{Fingerprint: fingerprint, KeyType: keyType})
}

type createServerRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthMethod string `json:"auth_method"`

	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase"`

	SudoPassword string `json:"sudo_password"`
	DockerSocket string `json:"docker_socket"`

	// HostKeyFingerprint is what the user confirmed in the previous step.
	HostKeyFingerprint string `json:"host_key_fingerprint"`
}

type serverResponse struct {
	store.Server
	// HasSudoPassword reports whether one is stored, without revealing it.
	HasSudoPassword bool `json:"has_sudo_password"`
}

func newServerResponse(server store.Server) serverResponse {
	return serverResponse{Server: server, HasSudoPassword: server.SudoPasswordSecretID != nil}
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) error {
	var req createServerRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	host := f.required("host", req.Host, 1, 255)
	username := f.required("username", req.Username, 1, 64)

	method := sshx.AuthMethod(req.AuthMethod)
	if !method.Valid() {
		f.add("auth_method", "Choose either password or key authentication.")
	}
	switch method {
	case sshx.AuthPassword:
		if req.Password == "" {
			f.add("password", "A password is required for password authentication.")
		}
	case sshx.AuthKey:
		if strings.TrimSpace(req.PrivateKey) == "" {
			f.add("private_key", "A private key is required for key authentication.")
		}
	}

	if req.Port < 0 || req.Port > 65535 {
		f.add("port", "Port must be between 1 and 65535.")
	}
	if strings.TrimSpace(req.HostKeyFingerprint) == "" {
		f.add("host_key_fingerprint", "Confirm the host key fingerprint before adding this server.")
	}
	if err := f.err(); err != nil {
		return err
	}

	actor := MustIdentity(r.Context())
	created, err := s.Store.CreateServer(r.Context(), s.Sealer, store.NewServer{
		Name:               name,
		Host:               host,
		Port:               req.Port,
		Username:           username,
		AuthMethod:         method,
		Password:           req.Password,
		PrivateKey:         req.PrivateKey,
		Passphrase:         req.Passphrase,
		SudoPassword:       req.SudoPassword,
		HostKeyFingerprint: strings.TrimSpace(req.HostKeyFingerprint),
		DockerSocket:       strings.TrimSpace(req.DockerSocket),
		CreatedBy:          actor.User.ID,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Invalid(fields{"name": "A server with that name already exists."})
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "servers", created.ID)
	AuditMeta(r.Context(), "host", created.Host)

	// Probe immediately so the wizard can report what was found. A failure
	// here is reported alongside the server rather than rolling back: the
	// record is valid and the user may simply need to fix sshd or permissions.
	caps, probeErr := s.Servers.Probe(r.Context(), created)
	if probeErr != nil {
		s.Log.Info("server added but probe failed", "server", created.ID, "error", probeErr)
	}

	refreshed, err := s.Store.ServerByID(r.Context(), created.ID)
	if err != nil {
		return Internal(err)
	}

	return JSON(w, s.Log, http.StatusCreated, map[string]any{
		"server":       newServerResponse(*refreshed),
		"capabilities": caps,
	})
}

// --- reading and changing servers ---------------------------------------

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) error {
	actor := MustIdentity(r.Context())
	servers, err := s.Store.ListServers(r.Context(), actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}

	out := make([]serverResponse, 0, len(servers))
	for _, server := range servers {
		out = append(out, newServerResponse(server))
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"servers": out})
}

// requireServer loads a server and checks the caller may reach it.
//
// Membership is checked here rather than in each handler because every server
// route needs it, and a route that forgot would expose another team's machine.
func (s *Server) requireServer(r *http.Request) (*store.Server, error) {
	id := chi.URLParam(r, "serverID")
	actor := MustIdentity(r.Context())

	server, err := s.Store.ServerByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, NotFound("No such server.")
		}
		return nil, Internal(err)
	}

	allowed, err := s.Store.CanAccessServer(r.Context(), id, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return nil, Internal(err)
	}
	if !allowed {
		// Same response as a missing server: whether a server exists is itself
		// information a user without access should not get.
		return nil, NotFound("No such server.")
	}
	return server, nil
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}
	return JSON(w, s.Log, http.StatusOK, newServerResponse(*server))
}

type updateServerRequest struct {
	Name         string `json:"name"`
	DockerSocket string `json:"docker_socket"`
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req updateServerRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}
	f := fields{}
	name := f.required("name", req.Name, 1, 100)
	if err := f.err(); err != nil {
		return err
	}

	socket := strings.TrimSpace(req.DockerSocket)
	if socket == "" {
		socket = server.DockerSocket
	}

	updated, err := s.Store.UpdateServer(r.Context(), server.ID, name, socket)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return Invalid(fields{"name": "A server with that name already exists."})
		}
		return Internal(err)
	}

	// The socket path is baked into the pooled Docker client, so a change has
	// to drop the connection or the old path keeps being used.
	if socket != server.DockerSocket {
		s.Servers.Evict(server.ID)
	}

	AuditResource(r.Context(), "servers", server.ID)
	return JSON(w, s.Log, http.StatusOK, newServerResponse(*updated))
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	if err := s.Store.DeleteServer(r.Context(), server.ID); err != nil {
		return Internal(err)
	}
	s.Servers.Evict(server.ID)

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "host", server.Host)
	AuditMeta(r.Context(), "name", server.Name)
	return NoContent(w)
}

// handleProbeServer re-checks a server on demand.
func (s *Server) handleProbeServer(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	caps, probeErr := s.Servers.Probe(ctx, server)

	// Report the failure as part of the server state rather than as a request
	// error: "we tried and here is what went wrong" is more useful on a status
	// page than a bare 502.
	refreshed, err := s.Store.ServerByID(r.Context(), server.ID)
	if err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "servers", server.ID)
	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"server":       newServerResponse(*refreshed),
		"capabilities": caps,
		"reachable":    probeErr == nil,
	})
}

func (s *Server) handleServerMetrics(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	metrics, err := s.Servers.Metrics(r.Context(), server)
	if err != nil {
		return Unavailable("Could not collect metrics from %s: %v", server.Name, err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, metrics)
}

// --- per-server access ---------------------------------------------------

func (s *Server) handleListServerMembers(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}
	members, err := s.Store.ListServerMembers(r.Context(), server.ID)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"members": members})
}

type grantServerRequest struct {
	UserID     string `json:"user_id"`
	Permission string `json:"permission"`
}

func (s *Server) handleGrantServerAccess(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req grantServerRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	permission := store.ServerPermission(req.Permission)
	if permission != store.ServerRead && permission != store.ServerOperate {
		return Invalid(fields{"permission": "Permission must be read or operate."})
	}
	if req.UserID == "" {
		return Invalid(fields{"user_id": "Choose a user."})
	}

	if err := s.Store.GrantServerAccess(r.Context(), server.ID, req.UserID, permission); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such user.")
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "granted_to", req.UserID)
	AuditMeta(r.Context(), "permission", req.Permission)
	return NoContent(w)
}

func (s *Server) handleRevokeServerAccess(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}
	userID := chi.URLParam(r, "userID")

	if err := s.Store.RevokeServerAccess(r.Context(), server.ID, userID); err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "revoked_from", userID)
	return NoContent(w)
}

type setServerSudoPasswordRequest struct {
	SudoPassword string `json:"sudo_password"`
}

func (s *Server) handleSetServerSudoPassword(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req setServerSudoPasswordRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	if err := s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword); err != nil {
		return Internal(err)
	}

	// Trigger async probe in background to update server.Capabilities
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if srv, err := s.Store.ServerByID(ctx, server.ID); err == nil {
			_, _ = s.Servers.Probe(ctx, srv)
		}
	}()

	refreshed, err := s.Store.ServerByID(r.Context(), server.ID)
	if err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "action", "set_sudo_password")
	return JSON(w, s.Log, http.StatusOK, newServerResponse(*refreshed))
}

type execRootRequest struct {
	Command      string `json:"command"`
	SudoPassword string `json:"sudo_password"`
	SaveSudo     bool   `json:"save_sudo"`
}

type execRootResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Success  bool   `json:"success"`
}

func (s *Server) handleExecRoot(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	var req execRootRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	cmd := strings.TrimSpace(req.Command)
	if cmd == "" {
		return Invalid(fields{"command": "Command cannot be empty."})
	}

	// If save_sudo is requested and password provided, persist it
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	conn, err := s.Servers.Connect(r.Context(), server)
	if err != nil {
		return Internal(fmt.Errorf("connect to server: %w", err))
	}
	defer conn.Release()

	cred, err := s.Store.ServerCredential(r.Context(), s.Sealer, server)
	if err != nil {
		return Internal(fmt.Errorf("read server credentials: %w", err))
	}

	sudoPass := req.SudoPassword
	if sudoPass == "" {
		sudoPass = cred.SudoPassword
	}

	var caps sshx.Capabilities
	if len(server.Capabilities) > 0 {
		_ = json.Unmarshal(server.Capabilities, &caps)
	}

	// Execute elevated command
	var fullCmd string
	if caps.SudoMode == sshx.SudoRoot {
		fullCmd = cmd
	} else if sudoPass != "" {
		fullCmd = fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' sh -c %s", shellQuote(sudoPass), shellQuote(cmd))
	} else if caps.SudoMode == sshx.SudoNoPassword {
		fullCmd = "sudo -n sh -c " + shellQuote(cmd)
	} else {
		return Invalid(fields{"sudo_password": "Sudo password is required to execute root command on this server."})
	}

	res, runErr := conn.Run(r.Context(), fullCmd)
	if runErr != nil && res.ExitCode == 0 {
		return Internal(fmt.Errorf("execute command: %w", runErr))
	}

	AuditResource(r.Context(), "servers", server.ID)
	AuditMeta(r.Context(), "action", "exec_root")
	AuditMeta(r.Context(), "command", cmd)

	return JSON(w, s.Log, http.StatusOK, execRootResponse{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
		Success:  res.Ok(),
	})
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
