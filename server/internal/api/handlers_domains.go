package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/nginxx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

var hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

type domainResponse struct {
	store.Domain
	ServerName     string `json:"server_name"`
	DeploymentName string `json:"deployment_name,omitempty"`
}

func (s *Server) handleListDomains(w http.ResponseWriter, r *http.Request) error {
	actor := MustIdentity(r.Context())
	serverID := r.URL.Query().Get("server_id")
	deploymentID := r.URL.Query().Get("deployment_id")

	filter := store.DomainFilter{}
	if serverID != "" {
		filter.ServerID = &serverID
	}
	if deploymentID != "" {
		filter.DeploymentID = &deploymentID
	}

	domains, err := s.Store.ListDomains(r.Context(), filter)
	if err != nil {
		return Internal(err)
	}

	// Filter by server access for non-admins
	servers, err := s.Store.ListServers(r.Context(), actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	serverNames := make(map[string]string, len(servers))
	accessibleServers := make(map[string]bool, len(servers))
	for _, sv := range servers {
		serverNames[sv.ID] = sv.Name
		accessibleServers[sv.ID] = true
	}

	deployments, _ := s.Store.ListDeployments(r.Context(), actor.User.ID, actor.Role() == auth.RoleAdmin)
	deploymentNames := make(map[string]string, len(deployments))
	for _, d := range deployments {
		deploymentNames[d.ID] = d.Name
	}

	out := make([]domainResponse, 0, len(domains))
	for _, d := range domains {
		if !accessibleServers[d.ServerID] {
			continue
		}
		if d.ConfigRendered == "" {
			if cfg, err := nginxx.Render(nginxx.Config{
				Hostname:     d.Hostname,
				UpstreamPort: d.UpstreamPort,
				WebSocket:    d.WebSocket,
				HasSSL:       d.SSLMode == store.DomainSSLLetsEncrypt,
			}); err == nil {
				d.ConfigRendered = cfg
			}
		}
		var depName string
		if d.DeploymentID != nil {
			depName = deploymentNames[*d.DeploymentID]
		}
		out = append(out, domainResponse{
			Domain:         d,
			ServerName:     serverNames[d.ServerID],
			DeploymentName: depName,
		})
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{"domains": out})
}

func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) error {
	domainID := chi.URLParam(r, "domainID")
	domain, err := s.Store.DomainByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("Domain not found.")
		}
		return Internal(err)
	}

	actor := MustIdentity(r.Context())
	allowed, err := s.Store.CanAccessServer(r.Context(), domain.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return NotFound("Domain not found.")
	}

	server, err := s.Store.ServerByID(r.Context(), domain.ServerID)
	if err != nil {
		return Internal(err)
	}

	if domain.ConfigRendered == "" {
		if cfg, err := nginxx.Render(nginxx.Config{
			Hostname:     domain.Hostname,
			UpstreamPort: domain.UpstreamPort,
			WebSocket:    domain.WebSocket,
			HasSSL:       domain.SSLMode == store.DomainSSLLetsEncrypt,
		}); err == nil {
			domain.ConfigRendered = cfg
		}
	}

	var depName string
	if domain.DeploymentID != nil {
		if dep, err := s.Store.DeploymentByID(r.Context(), *domain.DeploymentID); err == nil {
			depName = dep.Name
		}
	}

	return JSON(w, s.Log, http.StatusOK, domainResponse{
		Domain:         *domain,
		ServerName:     server.Name,
		DeploymentName: depName,
	})
}

type createDomainRequest struct {
	ServerID     string `json:"server_id"`
	DeploymentID string `json:"deployment_id"`
	Hostname     string `json:"hostname"`
	UpstreamPort int    `json:"upstream_port"`
	WebSocket    bool   `json:"websocket"`
	SSLMode      string `json:"ssl_mode"`
	SudoPassword string `json:"sudo_password"`
	SaveSudo     bool   `json:"save_sudo"`
}

func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) error {
	var req createDomainRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	f := fields{}
	hostname := strings.ToLower(strings.TrimSpace(req.Hostname))
	if hostname == "" {
		f.add("hostname", "Hostname is required.")
	} else if len(hostname) > 253 || !hostnamePattern.MatchString(hostname) {
		f.add("hostname", "Enter a valid domain name (e.g. app.example.com).")
	}

	if req.ServerID == "" && req.DeploymentID == "" {
		f.add("server_id", "Choose a server or deployment.")
	}

	sslMode := store.DomainSSLMode(req.SSLMode)
	if sslMode == "" {
		sslMode = store.DomainSSLNone
	}
	if !sslMode.Valid() {
		f.add("ssl_mode", "Choose either none or letsencrypt.")
	}

	actor := MustIdentity(r.Context())

	var deployment *store.Deployment
	if req.DeploymentID != "" {
		dep, err := s.Store.DeploymentByID(r.Context(), req.DeploymentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				f.add("deployment_id", "Deployment not found.")
			} else {
				return Internal(err)
			}
		} else {
			deployment = dep
			if req.ServerID == "" {
				req.ServerID = dep.ServerID
			} else if req.ServerID != dep.ServerID {
				f.add("deployment_id", "Deployment is not on the chosen server.")
			}
			if req.UpstreamPort == 0 && dep.HostPort != nil {
				req.UpstreamPort = *dep.HostPort
			}
		}
	}

	if req.UpstreamPort < 1 || req.UpstreamPort > 65535 {
		f.add("upstream_port", "Enter a valid port number between 1 and 65535.")
	}

	if err := f.err(); err != nil {
		return err
	}

	allowed, err := s.Store.CanAccessServer(r.Context(), req.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return Invalid(fields{"server_id": "Choose a server you have access to."})
	}

	server, err := s.Store.ServerByID(r.Context(), req.ServerID)
	if err != nil {
		return Internal(err)
	}

	var depID *string
	if deployment != nil {
		depID = &deployment.ID
	}

	// Persist sudo password if requested
	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	// Pre-render virtual host configuration so it is immediately available and never blank
	rendered, _ := nginxx.Render(nginxx.Config{
		Hostname:     hostname,
		UpstreamPort: req.UpstreamPort,
		WebSocket:    req.WebSocket,
		HasSSL:       sslMode == store.DomainSSLLetsEncrypt,
	})

	created, err := s.Store.CreateDomain(r.Context(), store.NewDomain{
		DeploymentID:   depID,
		ServerID:       server.ID,
		Hostname:       hostname,
		UpstreamPort:   req.UpstreamPort,
		SSLMode:        sslMode,
		WebSocket:      req.WebSocket,
		ConfigRendered: rendered,
		Status:         store.DomainStatusPending,
		StatusMessage:  "Deploying virtual host...",
		CreatedBy:      actor.User.ID,
	})
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return Invalid(fields{"hostname": "This domain is already registered."})
		}
		return Internal(err)
	}

	AuditResource(r.Context(), "domains", created.ID)
	AuditMeta(r.Context(), "hostname", created.Hostname)
	AuditMeta(r.Context(), "server_id", created.ServerID)

	// Deploy Nginx vhost on server
	if s.Nginx != nil {
		if sslMode == store.DomainSSLLetsEncrypt {
			// First deploy HTTP vhost, then attempt certificate issuance
			if _, deployErr := s.Nginx.Deploy(r.Context(), server, created, req.SudoPassword); deployErr != nil {
				_ = s.Store.UpdateDomainStatus(r.Context(), created.ID, store.UpdateDomainParams{
					ConfigRendered: &rendered,
					Status:        store.DomainStatusError,
					StatusMessage: deployErr.Error(),
				})
			} else {
				// Attempt SSL issuance
				if sslErr := s.Nginx.IssueSSL(r.Context(), server, created, req.SudoPassword); sslErr != nil {
					s.Log.Warn("ssl issuance failed after vhost created", "domain", hostname, "error", sslErr)
				}
			}
		} else {
			if _, deployErr := s.Nginx.Deploy(r.Context(), server, created, req.SudoPassword); deployErr != nil {
				_ = s.Store.UpdateDomainStatus(r.Context(), created.ID, store.UpdateDomainParams{
					ConfigRendered: &rendered,
					Status:        store.DomainStatusError,
					StatusMessage: deployErr.Error(),
				})
			}
		}
	}

	// Fetch refreshed record
	refreshed, err := s.Store.DomainByID(r.Context(), created.ID)
	if err == nil {
		created = refreshed
	}

	var depName string
	if deployment != nil {
		depName = deployment.Name
	}

	return JSON(w, s.Log, http.StatusCreated, domainResponse{
		Domain:         *created,
		ServerName:     server.Name,
		DeploymentName: depName,
	})
}

func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) error {
	domainID := chi.URLParam(r, "domainID")
	domain, err := s.Store.DomainByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("Domain not found.")
		}
		return Internal(err)
	}

	actor := MustIdentity(r.Context())
	allowed, err := s.Store.CanAccessServer(r.Context(), domain.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return NotFound("Domain not found.")
	}

	server, err := s.Store.ServerByID(r.Context(), domain.ServerID)
	if err != nil {
		return Internal(err)
	}

	if s.Nginx != nil {
		if err := s.Nginx.Remove(r.Context(), server, domain); err != nil {
			s.Log.Warn("failed to remove nginx config from server", "domain", domain.Hostname, "error", err)
		}
	} else {
		if err := s.Store.DeleteDomain(r.Context(), domain.ID); err != nil {
			return Internal(err)
		}
	}

	AuditResource(r.Context(), "domains", domain.ID)
	AuditMeta(r.Context(), "hostname", domain.Hostname)
	return NoContent(w)
}

func (s *Server) handleIssueSSL(w http.ResponseWriter, r *http.Request) error {
	domainID := chi.URLParam(r, "domainID")
	domain, err := s.Store.DomainByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("Domain not found.")
		}
		return Internal(err)
	}

	actor := MustIdentity(r.Context())
	allowed, err := s.Store.CanAccessServer(r.Context(), domain.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return NotFound("Domain not found.")
	}

	server, err := s.Store.ServerByID(r.Context(), domain.ServerID)
	if err != nil {
		return Internal(err)
	}

	if s.Nginx == nil {
		return Internal(errors.New("nginx manager not configured"))
	}

	domain.SSLMode = store.DomainSSLLetsEncrypt

	var req struct {
		SudoPassword string `json:"sudo_password"`
		SaveSudo     bool   `json:"save_sudo"`
	}
	if r.ContentLength > 0 {
		_ = DecodeJSON(w, r, &req)
	}

	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if err := s.Nginx.IssueSSL(r.Context(), server, domain, req.SudoPassword); err != nil {
		return Invalid(fields{"ssl": err.Error()})
	}

	refreshed, err := s.Store.DomainByID(r.Context(), domain.ID)
	if err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "domains", domain.ID)
	AuditMeta(r.Context(), "action", "issue_ssl")
	return JSON(w, s.Log, http.StatusOK, domainResponse{
		Domain:     *refreshed,
		ServerName: server.Name,
	})
}

func (s *Server) handleSyncDomain(w http.ResponseWriter, r *http.Request) error {
	domainID := chi.URLParam(r, "domainID")
	domain, err := s.Store.DomainByID(r.Context(), domainID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("Domain not found.")
		}
		return Internal(err)
	}

	actor := MustIdentity(r.Context())
	allowed, err := s.Store.CanAccessServer(r.Context(), domain.ServerID, actor.User.ID, actor.Role() == auth.RoleAdmin)
	if err != nil {
		return Internal(err)
	}
	if !allowed {
		return NotFound("Domain not found.")
	}

	server, err := s.Store.ServerByID(r.Context(), domain.ServerID)
	if err != nil {
		return Internal(err)
	}

	if s.Nginx == nil {
		return Internal(errors.New("nginx manager not configured"))
	}

	var req struct {
		SudoPassword string `json:"sudo_password"`
		SaveSudo     bool   `json:"save_sudo"`
	}
	if r.ContentLength > 0 {
		_ = DecodeJSON(w, r, &req)
	}

	if req.SaveSudo && req.SudoPassword != "" {
		_ = s.Store.SetServerSudoPassword(r.Context(), s.Sealer, server.ID, req.SudoPassword)
	}

	if _, err := s.Nginx.Deploy(r.Context(), server, domain, req.SudoPassword); err != nil {
		_ = s.Store.UpdateDomainStatus(r.Context(), domain.ID, store.UpdateDomainParams{
			Status:        store.DomainStatusError,
			StatusMessage: err.Error(),
		})
		return Invalid(fields{"sync": err.Error()})
	}

	if domain.SSLMode == store.DomainSSLLetsEncrypt {
		if err := s.Nginx.IssueSSL(r.Context(), server, domain, req.SudoPassword); err != nil {
			_ = s.Store.UpdateDomainStatus(r.Context(), domain.ID, store.UpdateDomainParams{
				Status:        store.DomainStatusError,
				StatusMessage: err.Error(),
			})
			return Invalid(fields{"ssl": err.Error()})
		}
	} else {
		_ = s.Store.UpdateDomainStatus(r.Context(), domain.ID, store.UpdateDomainParams{
			Status:        store.DomainStatusActive,
			StatusMessage: "",
		})
	}

	refreshed, err := s.Store.DomainByID(r.Context(), domain.ID)
	if err != nil {
		return Internal(err)
	}

	AuditResource(r.Context(), "domains", domain.ID)
	AuditMeta(r.Context(), "action", "sync_domain")
	return JSON(w, s.Log, http.StatusOK, domainResponse{
		Domain:     *refreshed,
		ServerName: server.Name,
	})
}
