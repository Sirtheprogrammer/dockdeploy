// Package api wires HTTP routing, middleware and handlers.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/config"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/deploy"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/discovery"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/nginxx"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/secrets"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/servers"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// Server holds the dependencies shared by every handler.
type Server struct {
	Config  *config.Config
	Log     *slog.Logger
	Store   *store.Store
	Sealer  *secrets.Sealer
	Hasher  *auth.Hasher
	Started time.Time

	// Servers turns a stored server row into a live SSH or Docker connection.
	Servers *servers.Manager
	// Deploys owns the build pipeline and the live log hub.
	Deploys *deploy.Engine
	// Nginx manages virtual hosts, TLS certificates and reloads.
	Nginx *nginxx.Manager
	// Discovery auto-detects domains, deployments, and credentials on servers.
	Discovery *discovery.Service

	// SPA serves the built frontend. Requests that do not match /api are
	// handed here, so the controller runs as a single container.
	SPA http.Handler

	// policies records the access rule each route declared. assertPolicies
	// refuses to start if a route is missing from it.
	policies map[string]policy
}

// Routes builds the HTTP handler for the whole application.
//
// It returns an error rather than panicking so a missing access policy stops
// the process at startup with a readable message.
func (s *Server) Routes() (http.Handler, error) {
	s.policies = map[string]policy{}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(s.Log))
	r.Use(recoverer(s.Log))
	r.Use(securityHeaders)
	// Note: no global request timeout. Log streaming, container stats and
	// exec terminals hold a request open for as long as the user watches, so
	// timeouts belong on individual non-streaming route groups instead.

	root := routes{server: s, router: r}

	root.group("/api", func(api routes) {
		api.router.Use(s.authenticate)
		api.router.Use(s.csrfProtect)
		api.router.Use(s.auditWrites)

		api.open(http.MethodGet, "/health", s.handleHealth)

		api.group("/auth", func(a routes) {
			a.router.Use(s.rateLimitAuth)
			a.open(http.MethodGet, "/setup", s.handleSetupStatus)
			a.open(http.MethodPost, "/setup", s.handleSetup)
			a.open(http.MethodPost, "/login", s.handleLogin)
			a.open(http.MethodPost, "/login/2fa", s.handleLogin2FA)
			a.open(http.MethodGet, "/invitations/{token}", s.handleInvitationPreview)
			a.open(http.MethodPost, "/invitations/{token}/accept", s.handleAcceptInvitation)

			a.guarded(http.MethodPost, "/logout", auth.PermSelf, s.handleLogout)
			a.guarded(http.MethodGet, "/me", auth.PermSelf, s.handleMe)
			a.guarded(http.MethodPatch, "/me", auth.PermSelf, s.handleUpdateMe)
			a.guarded(http.MethodPost, "/password", auth.PermSelf, s.handleChangePassword)
			a.guarded(http.MethodGet, "/sessions", auth.PermSelf, s.handleListSessions)
			a.guarded(http.MethodDelete, "/sessions/{sessionID}", auth.PermSelf, s.handleRevokeSession)

			a.guarded(http.MethodGet, "/2fa/status", auth.PermSelf, s.handle2FAStatus)
			a.guarded(http.MethodPost, "/2fa/setup", auth.PermSelf, s.handle2FASetup)
			a.guarded(http.MethodPost, "/2fa/enable", auth.PermSelf, s.handle2FAEnable)
			a.guarded(http.MethodPost, "/2fa/disable", auth.PermSelf, s.handle2FADisable)
			a.guarded(http.MethodPost, "/2fa/recovery-codes", auth.PermSelf, s.handle2FARegenerateRecoveryCodes)
		})

		// Tokens are personal: every signed-in user manages their own.
		api.group("/tokens", func(t routes) {
			t.guarded(http.MethodGet, "/", auth.PermSelf, s.handleListTokens)
			t.guarded(http.MethodPost, "/", auth.PermSelf, s.handleCreateToken)
			t.guarded(http.MethodDelete, "/{tokenID}", auth.PermSelf, s.handleDeleteToken)
		})

		api.group("/users", func(u routes) {
			u.guarded(http.MethodGet, "/", auth.PermUserRead, s.handleListUsers)
			u.guarded(http.MethodPatch, "/{userID}", auth.PermUserWrite, s.handleUpdateUser)
			u.guarded(http.MethodDelete, "/{userID}", auth.PermUserWrite, s.handleDeleteUser)
		})

		api.group("/invitations", func(i routes) {
			i.guarded(http.MethodGet, "/", auth.PermUserRead, s.handleListInvitations)
			i.guarded(http.MethodPost, "/", auth.PermUserWrite, s.handleCreateInvitation)
			i.guarded(http.MethodDelete, "/{invitationID}", auth.PermUserWrite, s.handleDeleteInvitation)
		})

		api.group("/servers", func(sv routes) {
			// Reading a host key is a prerequisite for adding a server, so it
			// needs the same permission as creating one.
			sv.guarded(http.MethodPost, "/fingerprint", auth.PermServerWrite, s.handleServerFingerprint)

			sv.guarded(http.MethodGet, "/", auth.PermServerRead, s.handleListServers)
			sv.guarded(http.MethodPost, "/", auth.PermServerWrite, s.handleCreateServer)
			sv.guarded(http.MethodPost, "/autodetect-all", auth.PermServerWrite, s.handleAutoDetectAllServers)
			sv.guarded(http.MethodGet, "/{serverID}", auth.PermServerRead, s.handleGetServer)
			sv.guarded(http.MethodPatch, "/{serverID}", auth.PermServerWrite, s.handleUpdateServer)
			sv.guarded(http.MethodDelete, "/{serverID}", auth.PermServerDelete, s.handleDeleteServer)
			sv.guarded(http.MethodPost, "/{serverID}/probe", auth.PermServerRead, s.handleProbeServer)
			sv.guarded(http.MethodPost, "/{serverID}/autodetect", auth.PermServerWrite, s.handleAutoDetectServer)

			// Granting access to a machine is user management, not server
			// operation, so it takes the stronger permission.
			sv.guarded(http.MethodGet, "/{serverID}/members", auth.PermUserRead, s.handleListServerMembers)
			sv.guarded(http.MethodPost, "/{serverID}/members", auth.PermUserWrite, s.handleGrantServerAccess)
			sv.guarded(http.MethodDelete, "/{serverID}/members/{userID}", auth.PermUserWrite, s.handleRevokeServerAccess)

			sv.guarded(http.MethodGet, "/{serverID}/docker", auth.PermServerRead, s.handleDockerInfo)
			sv.guarded(http.MethodGet, "/{serverID}/images", auth.PermServerRead, s.handleListImages)
			sv.guarded(http.MethodGet, "/{serverID}/volumes", auth.PermServerRead, s.handleListVolumes)
			sv.guarded(http.MethodGet, "/{serverID}/networks", auth.PermServerRead, s.handleListNetworks)

			sv.guarded(http.MethodGet, "/{serverID}/containers", auth.PermServerRead, s.handleListContainers)
			sv.guarded(http.MethodGet, "/{serverID}/containers/{containerID}", auth.PermServerRead, s.handleInspectContainer)
			sv.guarded(http.MethodGet, "/{serverID}/containers/{containerID}/logs", auth.PermServerRead, s.handleContainerLogs)
			sv.guarded(http.MethodPost, "/{serverID}/containers/{containerID}/actions", auth.PermContainerOperate, s.handleContainerAction)

			sv.guarded(http.MethodGet, "/{serverID}/metrics", auth.PermServerRead, s.handleServerMetrics)
			sv.guarded(http.MethodGet, "/{serverID}/terminal", auth.PermServerWrite, s.handleServerTerminal)

			sv.guarded(http.MethodGet, "/{serverID}/files", auth.PermServerRead, s.handleListFiles)
			sv.guarded(http.MethodGet, "/{serverID}/files/download", auth.PermServerRead, s.handleDownloadFile)
			sv.guarded(http.MethodGet, "/{serverID}/files/archive", auth.PermServerRead, s.handleDownloadArchive)
			sv.guarded(http.MethodGet, "/{serverID}/files/content", auth.PermServerRead, s.handleGetFileContent)
			sv.guarded(http.MethodPut, "/{serverID}/files/content", auth.PermServerWrite, s.handleSaveFileContent)
			sv.guarded(http.MethodPost, "/{serverID}/files/upload", auth.PermServerWrite, s.handleUploadFile)
			sv.guarded(http.MethodPost, "/{serverID}/files/transfer", auth.PermServerWrite, s.handleTransferFile)

			sv.guarded(http.MethodPost, "/{serverID}/sudo-password", auth.PermServerWrite, s.handleSetServerSudoPassword)
			sv.guarded(http.MethodPost, "/{serverID}/exec-root", auth.PermServerWrite, s.handleExecRoot)
			sv.guarded(http.MethodPost, "/{serverID}/install-docker", auth.PermServerWrite, s.handleInstallDocker)

			sv.guarded(http.MethodGet, "/{serverID}/database-backups", auth.PermServerRead, s.handleListDatabaseBackups)
			sv.guarded(http.MethodPost, "/{serverID}/database-backups", auth.PermServerWrite, s.handleCreateDatabaseBackup)
			sv.guarded(http.MethodPost, "/{serverID}/database-backups/restore", auth.PermServerWrite, s.handleRestoreDatabaseBackup)
			sv.guarded(http.MethodDelete, "/{serverID}/database-backups", auth.PermServerWrite, s.handleDeleteDatabaseBackup)
		})

		api.group("/deployments", func(d routes) {
			d.guarded(http.MethodGet, "/", auth.PermDeploymentRead, s.handleListDeployments)
			d.guarded(http.MethodPost, "/", auth.PermDeploymentWrite, s.handleCreateDeployment)
			d.guarded(http.MethodGet, "/{deploymentID}", auth.PermDeploymentRead, s.handleGetDeployment)
			d.guarded(http.MethodDelete, "/{deploymentID}", auth.PermDeploymentDelete, s.handleDeleteDeployment)

			d.guarded(http.MethodGet, "/{deploymentID}/env", auth.PermDeploymentRead, s.handleGetDeploymentEnv)
			d.guarded(http.MethodPut, "/{deploymentID}/env", auth.PermDeploymentWrite, s.handleSetDeploymentEnv)

			d.guarded(http.MethodGet, "/{deploymentID}/runs", auth.PermDeploymentRead, s.handleListRuns)
			d.guarded(http.MethodPost, "/{deploymentID}/runs", auth.PermDeploymentDeploy, s.handleDeploy)
			d.guarded(http.MethodGet, "/{deploymentID}/runs/{runID}/logs", auth.PermDeploymentRead, s.handleRunLogs)
			d.guarded(http.MethodPost, "/{deploymentID}/runs/{runID}/cancel", auth.PermDeploymentDeploy, s.handleCancelRun)

			d.guarded(http.MethodGet, "/{deploymentID}/webhook", auth.PermDeploymentWrite, s.handleGetDeploymentWebhook)
			d.guarded(http.MethodPost, "/{deploymentID}/webhook/rotate", auth.PermDeploymentWrite, s.handleRotateDeploymentWebhook)
			d.open(http.MethodPost, "/{deploymentID}/webhook", s.handleTriggerWebhook)
		})

		api.group("/domains", func(dm routes) {
			dm.guarded(http.MethodGet, "/", auth.PermDomainRead, s.handleListDomains)
			dm.guarded(http.MethodPost, "/", auth.PermDomainWrite, s.handleCreateDomain)
			dm.guarded(http.MethodGet, "/{domainID}", auth.PermDomainRead, s.handleGetDomain)
			dm.guarded(http.MethodDelete, "/{domainID}", auth.PermDomainWrite, s.handleDeleteDomain)
			dm.guarded(http.MethodPost, "/{domainID}/ssl", auth.PermDomainWrite, s.handleIssueSSL)
			dm.guarded(http.MethodPost, "/{domainID}/sync", auth.PermDomainWrite, s.handleSyncDomain)
		})

		// Registry and git credentials are shared infrastructure, so they need
		// the credential permission rather than the deployment one.
		api.group("/registries", func(g routes) {
			g.guarded(http.MethodGet, "/", auth.PermCredentialRead, s.handleListRegistries)
			g.guarded(http.MethodPost, "/", auth.PermCredentialWrite, s.handleCreateRegistry)
			g.guarded(http.MethodDelete, "/{registryID}", auth.PermCredentialWrite, s.handleDeleteRegistry)
		})

		api.group("/git-credentials", func(g routes) {
			g.guarded(http.MethodGet, "/", auth.PermCredentialRead, s.handleListGitCredentials)
			g.guarded(http.MethodPost, "/", auth.PermCredentialWrite, s.handleCreateGitCredential)
			g.guarded(http.MethodDelete, "/{credentialID}", auth.PermCredentialWrite, s.handleDeleteGitCredential)
		})

		api.guarded(http.MethodGet, "/audit", auth.PermAuditRead, s.handleListAudit)

		api.router.NotFound(s.wrap(func(http.ResponseWriter, *http.Request) error {
			return NotFound("No such endpoint.")
		}))
		api.router.MethodNotAllowed(s.wrap(func(_ http.ResponseWriter, r *http.Request) error {
			return MethodNotAllowed(r.Method)
		}))
	})

	if s.SPA != nil {
		r.Handle("/*", s.SPA)
	}

	if err := assertPolicies(r, s.policies); err != nil {
		return nil, err
	}
	s.Log.Debug("route policies verified", "routes", len(s.policies))
	return r, nil
}

// wrap is shorthand for Wrap bound to this server's logger.
func (s *Server) wrap(h Handler) http.HandlerFunc { return Wrap(s.Log, h) }
