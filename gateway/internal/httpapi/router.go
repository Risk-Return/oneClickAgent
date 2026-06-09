// Package httpapi registers all REST + WS routes on a chi router,
// applies the middleware chain, and wires handlers to store/tunnel/pubsub/pool.
package httpapi

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/config"
	"github.com/oneClickAgent/gateway/internal/credvault"
	"github.com/oneClickAgent/gateway/internal/obs"
	"github.com/oneClickAgent/gateway/internal/pool"
	"github.com/oneClickAgent/gateway/internal/pubsub"
	"github.com/oneClickAgent/gateway/internal/relay"
	"github.com/oneClickAgent/gateway/internal/skillvault"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
	"github.com/oneClickAgent/gateway/internal/vncrelay"
)

// Dependencies holds all services needed by the HTTP API.
type Dependencies struct {
	Config    config.Config
	DB        *store.DB
	Hub       *tunnel.Hub
	Broker    *pubsub.Broker
	Allocator *pool.Allocator
	Relay     *relay.FileRelay
	Vault     *skillvault.Vault
	Dispatch  *skillvault.Dispatcher
	JWT       *auth.JWTManager
	Hasher    *auth.PasswordHasher

	// VNC & Credentials
	VNCRelay  *vncrelay.Relay
	CredVault *credvault.Vault

	// Stores
	Users   store.UserStoreInterface
	Tokens  store.TokenStoreInterface
	Devices store.DeviceStoreInterface
	Agents  store.AgentStoreInterface
	Jobs    store.JobStoreInterface
	Files   store.FileStoreInterface
	Skills  store.SkillStoreInterface
	Orgs    store.OrgStoreInterface
	Audit   *store.AuditStore
	Creds   *store.CredentialStore
	VNC     *store.VNCSessionStore
}

// NewRouter creates and configures the chi router with all routes.
func NewRouter(deps *Dependencies) chi.Router {
	r := chi.NewRouter()

	// Middleware
	r.Use(requestIDMiddleware)
	r.Use(recoverMiddleware)
	r.Use(securityHeadersMiddleware)
	r.Use(corsMiddleware(deps.Config.CORSAllowedOrigins))
	r.Use(csrfMiddleware(deps.Config.CORSAllowedOrigins))
	r.Use(rateLimitMiddleware(deps.Config.RateLimitAPIPerSec))
	r.Use(loggerMiddleware)

	// Public routes (with auth-specific rate limiting)
	r.Group(func(r chi.Router) {
		r.Use(authRateLimitMiddleware(deps.Config.RateLimitAuthPerMin))
		r.Post("/api/v1/auth/register", deps.handleRegister())
		r.Post("/api/v1/auth/login", deps.handleLogin())
		r.Post("/api/v1/auth/refresh", deps.handleRefresh())
	})

	// Device enrollment (device self-enrollment, no JWT required)
	r.Post("/api/v1/devices/enroll", deps.handleDeviceEnroll())

	// Health & metrics
	r.Get("/healthz", handleHealthz(deps.DB))
	r.Get("/readyz", handleReadyz(deps.DB))
	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		obs.MetricsHandler().ServeHTTP(w, r)
	})

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(deps.JWT))
		r.Use(tenantScopeMiddleware(deps.Jobs, deps.Files))
		r.Use(idempotencyMiddleware)

		// Auth
		r.Post("/api/v1/auth/logout", deps.handleLogout())
		r.Get("/api/v1/auth/me", deps.handleMe())

		// Devices (admin only)
		r.Group(func(r chi.Router) {
			r.Use(requireAdminMiddleware)
			r.Post("/api/v1/devices", deps.handleCreateDevice())
			r.Get("/api/v1/devices", deps.handleListDevices())
			r.Get("/api/v1/devices/{deviceID}", deps.handleGetDevice())
			r.Patch("/api/v1/devices/{deviceID}", deps.handleUpdateDevice())
			r.Delete("/api/v1/devices/{deviceID}", deps.handleDeleteDevice())
			r.Post("/api/v1/devices/{deviceID}/rotate-token", deps.handleRotateDeviceToken())
			r.Post("/api/v1/admin/devices/{deviceID}/pool", deps.handleSetPoolSize())
		})

		// Agent pool (admin managed)
		r.Get("/api/v1/agents", deps.handleListMyAgents())
		r.Get("/api/v1/agents/{agentID}", deps.handleGetAgent())
		r.Post("/api/v1/agents/{agentID}/skills", deps.handleEnableAgentSkill())
		r.Delete("/api/v1/agents/{agentID}/skills/{skillID}", deps.handleDisableAgentSkill())

		r.Group(func(r chi.Router) {
			r.Use(requireAdminMiddleware)
			r.Get("/api/v1/admin/agents", deps.handleAdminListAgents())
			r.Get("/api/v1/admin/agents/{agentID}", deps.handleAdminGetAgent())
			r.Delete("/api/v1/admin/agents/{agentID}", deps.handleAdminDeleteAgent())
			r.Post("/api/v1/admin/agents/{agentID}/drain", deps.handleDrainAgent())
			r.Post("/api/v1/admin/agents/{agentID}/release", deps.handleForceReleaseAgent())
		})

		// Jobs
		r.Group(func(r chi.Router) {
			r.Use(jobRateLimitMiddleware(deps.Config.RateLimitJobSubmitPerMin))
			r.Post("/api/v1/jobs", deps.handleSubmitJob())
		})
		r.Get("/api/v1/jobs", deps.handleListJobs())
		r.Get("/api/v1/jobs/{jobID}", deps.handleGetJob())
		r.Post("/api/v1/jobs/{jobID}/cancel", deps.handleCancelJob())
		r.Get("/api/v1/jobs/{jobID}/result", deps.handleGetJobResult())
		r.Get("/api/v1/jobs/{jobID}/output", deps.handleListJobOutputs())
		r.Get("/api/v1/jobs/{jobID}/output/{fileID}", deps.handleDownloadJobOutput())

		// VNC sessions
		r.Post("/api/v1/jobs/{jobID}/vnc", deps.handleOpenVNC())
		r.Get("/api/v1/jobs/{jobID}/vnc", deps.handleGetJobVNC())
		r.Post("/api/v1/vnc/{sessionID}/save-login", deps.handleSaveLogin())
		r.Delete("/api/v1/vnc/{sessionID}", deps.handleCloseVNC())

		// Credentials
		r.Get("/api/v1/credentials", deps.handleListCredentials())
		r.Get("/api/v1/credentials/{credentialID}", deps.handleGetCredential())
		r.Patch("/api/v1/credentials/{credentialID}", deps.handleUpdateCredential())
		r.Delete("/api/v1/credentials/{credentialID}", deps.handleDeleteCredential())

		// Files
		r.Post("/api/v1/files", deps.handleUploadFile())
		r.Get("/api/v1/files", deps.handleListFiles())
		r.Get("/api/v1/files/{fileID}", deps.handleGetFile())
		r.Delete("/api/v1/files/{fileID}", deps.handleDeleteFile())

		// Skills
		r.Get("/api/v1/skills", deps.handleListVisibleSkills())
		r.Get("/api/v1/skills/{skillID}", deps.handleGetSkill())

		r.Group(func(r chi.Router) {
			r.Use(requireAdminMiddleware)
			r.Get("/api/v1/admin/skills", deps.handleAdminListSkills())
			r.Post("/api/v1/admin/skills", deps.handleCreateSkill())
			r.Get("/api/v1/admin/skills/{skillID}", deps.handleAdminGetSkill())
			r.Patch("/api/v1/admin/skills/{skillID}", deps.handleUpdateSkill())
			r.Delete("/api/v1/admin/skills/{skillID}", deps.handleDeleteSkill())
			r.Post("/api/v1/admin/skills/{skillID}/versions", deps.handlePublishSkillVersion())
			r.Post("/api/v1/admin/skills/{skillID}/install", deps.handleInstallSkillFleet())
			r.Delete("/api/v1/admin/skills/{skillID}/install", deps.handleDeleteSkillFleet())
			r.Post("/api/v1/admin/skills/{skillID}/disable", deps.handleDisableSkillFleet())
			r.Post("/api/v1/admin/skills/{skillID}/enable", deps.handleEnableSkillFleet())
			r.Post("/api/v1/admin/skills/{skillID}/update", deps.handleUpdateSkillFleet())
			r.Post("/api/v1/admin/skills/{skillID}/retry", deps.handleRetrySkillFleet())
			r.Get("/api/v1/admin/skills/{skillID}/rollout", deps.handleGetSkillRollout())
			r.Patch("/api/v1/admin/skills/{skillID}/visibility", deps.handleUpdateSkillVisibility())
			r.Get("/api/v1/admin/skills/{skillID}/grants", deps.handleListSkillGrants())
			r.Post("/api/v1/admin/skills/{skillID}/grants", deps.handleCreateSkillGrant())
			r.Delete("/api/v1/admin/skills/{skillID}/grants", deps.handleDeleteSkillGrant())
			r.Delete("/api/v1/admin/skills/{skillID}/grants/{principal_type}/{principal_id}", deps.handleDeleteSkillGrantPath())
		})

		// Organizations (admin)
		r.Group(func(r chi.Router) {
			r.Use(requireAdminMiddleware)
			r.Post("/api/v1/admin/orgs", deps.handleCreateOrg())
			r.Get("/api/v1/admin/orgs", deps.handleListOrgs())
			r.Get("/api/v1/admin/orgs/{orgID}", deps.handleGetOrg())
			r.Get("/api/v1/admin/orgs/{orgID}/members", deps.handleListOrgMembers())
			r.Patch("/api/v1/admin/orgs/{orgID}", deps.handleUpdateOrg())
			r.Delete("/api/v1/admin/orgs/{orgID}", deps.handleDeleteOrg())
			r.Post("/api/v1/admin/orgs/{orgID}/members", deps.handleAddOrgMember())
			r.Delete("/api/v1/admin/orgs/{orgID}/members/{userID}", deps.handleRemoveOrgMember())
		})

		// User tier management (admin)
		r.Group(func(r chi.Router) {
			r.Use(requireAdminMiddleware)
			r.Get("/api/v1/admin/users", deps.handleListUsers())
			r.Patch("/api/v1/admin/users/{userID}/tier", deps.handleUpdateUserTier())
		})
	})

	// WebSocket realtime
	r.Get("/ws", deps.handleWebSocket())

	// Tunnel WebSocket (device → gateway)
	r.Get("/tunnel", deps.handleTunnel())

	// VNC WebSocket endpoints (custom auth)
	r.Get("/ws/vnc/{sessionID}", deps.handleVNCBrowserSocket())
	r.Get("/session/{sessionID}", deps.handleVNCDeviceSocket())

	// Web frontend SPA (serve static files, fallback to index.html)
	if deps.Config.WebDistDir != "" {
		r.Handle("/*", spaFileServer(deps.Config.WebDistDir))
	}

	return r
}

// corsMiddleware returns a CORS middleware with the configured allowed origins.
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}
	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}

// spaFileServer serves a static SPA with fallback to index.html for client-side routes.
func spaFileServer(distDir string) http.HandlerFunc {
	fs := http.FileServer(http.Dir(distDir))
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(distDir, filepath.Clean(r.URL.Path))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// SPA fallback: serve index.html for any non-file route
			r.URL.Path = "/"
		}
		fs.ServeHTTP(w, r)
	}
}
