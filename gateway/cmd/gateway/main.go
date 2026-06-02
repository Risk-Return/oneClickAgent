// Main entry point for the oneClickAgent Cloud Gateway.
// Wires config, initialises store/tunnel hub/pubsub/HTTP server,
// and handles graceful shutdown (SIGINT/SIGTERM).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/config"
	"github.com/oneClickAgent/gateway/internal/credvault"
	"github.com/oneClickAgent/gateway/internal/httpapi"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
	"github.com/oneClickAgent/gateway/internal/pool"
	"github.com/oneClickAgent/gateway/internal/pubsub"
	"github.com/oneClickAgent/gateway/internal/relay"
	"github.com/oneClickAgent/gateway/internal/skillvault"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
	"github.com/oneClickAgent/gateway/internal/vncrelay"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialise structured logging
	logger := obs.InitLogger(cfg.LogLevel, cfg.LogFormat)
	logger.Info("starting oneClickAgent Cloud Gateway",
		"env", cfg.Env,
		"addr", cfg.HTTPAddr,
	)

	// Connect to PostgreSQL
	ctx := context.Background()
	db, err := store.NewDB(ctx, cfg.DBURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("connected to PostgreSQL")

	// Initialise stores
	users := store.NewUserStore(db)
	tokens := store.NewTokenStore(db)
	devices := store.NewDeviceStore(db)
	agents := store.NewAgentStore(db)
	jobs := store.NewJobStore(db)
	files := store.NewFileStore(db)
	skills := store.NewSkillStore(db)
	orgs := store.NewOrgStore(db)
	audit := store.NewAuditStore(db)

	// Auth
	jwtManager := auth.NewJWTManager(cfg.JWTSecret, cfg.AccessTTL)
	hasher := auth.NewPasswordHasher()

	// PubSub broker
	broker := pubsub.NewBroker()

	// Tunnel Hub
	tunnelHub := tunnel.NewHub(tunnel.HubConfig{
		HeartbeatInterval:      cfg.HeartbeatInterval,
		HeartbeatMissThreshold: cfg.HeartbeatMissThreshold,
	})

	// Agent Pool Allocator
	allocator := pool.NewAllocator(
		agents,
		jobs,
		tunnelHub,
		broker,
		cfg.QueueTTL,
		cfg.MaxQueuedPerUser,
	)

	// File Relay
	fileRelay := relay.NewFileRelay(
		files,
		tunnelHub,
		cfg.FileStore,
		cfg.MaxUploadBytes,
		time.Duration(cfg.FileRetentionHours)*time.Hour,
	)

	// Skill Vault
	vault := skillvault.NewVault(skills, cfg.FileStore+"/skills")
	skillDispatch := skillvault.NewDispatcher(vault, skills, tunnelHub)
	skillDispatch.SetDeviceLister(func(ctx context.Context) ([]model.Device, error) {
		return devices.ListAll(ctx)
	})

	// Credential Vault
	credVault, err := credvault.NewVault(cfg.CredKey, cfg.CredKMS)
	if err != nil {
		logger.Error("failed to initialize credential vault", "error", err)
		os.Exit(1)
	}

	// VNC Relay
	vncStore := store.NewVNCSessionStore(db)
	credStore := store.NewCredentialStore(db)
	vncRelay := vncrelay.NewRelay(
		tunnelHub.NodeID(),
		int64(cfg.VNCSessionBufBytes),
		cfg.VNCMaxSessionsPerUser,
	)

	// Wire tunnel hub handlers
	tunnelHub.SetHandlers(tunnel.HubConfig{
		OnJobProgress: func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error {
			if err := jobs.UpdateProgress(ctx, payload.JobID, payload.Percent, payload.Message); err != nil {
				return err
			}
			broker.Publish(pubsub.JobTopic(payload.JobID), model.WSEvent{
				Type:  model.WSEventJobProgress,
				Topic: pubsub.JobTopic(payload.JobID),
			})
			return nil
		},
		OnJobResult: func(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error {
			var result *json.RawMessage
			if payload.Result != nil {
				r := json.RawMessage(*payload.Result)
				result = &r
			}
			if err := jobs.UpdateResult(ctx, payload.JobID, payload.Status, result); err != nil {
				return err
			}
			broker.Publish(pubsub.JobTopic(payload.JobID), model.WSEvent{
				Type:  model.WSEventJobResult,
				Topic: pubsub.JobTopic(payload.JobID),
			})
			// Release agent back to pool
			job, err := jobs.GetByID(ctx, payload.JobID)
			if err == nil && job != nil && job.AgentID != nil {
				_ = allocator.Release(ctx, *job.AgentID)
				// Cleanup files
				_ = fileRelay.CleanupJobFiles(ctx, payload.JobID)
			}
			return nil
		},
		OnAgentStatus: func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error {
			return agents.UpdateStatus(ctx, payload.AgentID, payload.Status)
		},
		OnSkillState: func(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error {
			return skillDispatch.UpdateDeviceSkillState(ctx, payload)
		},
		OnFileAck: func(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error {
			return fileRelay.OnFileAck(ctx, payload)
		},
		OnVNCOpened: func(ctx context.Context, deviceID model.UUID, payload model.VNCOpenedPayload) error {
			if payload.Status == "ready" {
				return vncRelay.MarkReady(payload.SessionID, payload.RFBPassword)
			}
			vncRelay.CloseSession(payload.SessionID, "device reported error: "+payload.Error)
			return nil
		},
		OnCredPushAck: func(ctx context.Context, deviceID model.UUID, payload model.CredPushAckPayload) error {
			if payload.Status == "ok" {
				return credStore.Touch(ctx, payload.CredentialID)
			}
			slog.Error("credential push failed", "job_id", payload.JobID, "cred_id", payload.CredentialID, "error", payload.Error)
			return nil
		},
		OnCredCaptureAck: func(ctx context.Context, deviceID model.UUID, payload model.CredCaptureAckPayload) error {
			if payload.Status != "ok" {
				slog.Error("credential capture failed", "session_id", payload.SessionID, "error", payload.Error)
			}
			return nil
		},
	})

	// Assemble HTTP dependencies
	deps := &httpapi.Dependencies{
		Config:    cfg,
		DB:        db,
		Hub:       tunnelHub,
		Broker:    broker,
		Allocator: allocator,
		Relay:     fileRelay,
		Vault:     vault,
		Dispatch:  skillDispatch,
		JWT:       jwtManager,
		Hasher:    hasher,
		Users:     users,
		Tokens:    tokens,
		Devices:   devices,
		Agents:    agents,
		Jobs:      jobs,
		Files:     files,
		Skills:    skills,
		Orgs:      orgs,
		Audit:     audit,
		VNCRelay:  vncRelay,
		CredVault: credVault,
		Creds:     credStore,
		VNC:       vncStore,
	}

	// Create router
	router := httpapi.NewRouter(deps)

	// HTTP server
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("HTTP server listening", "addr", cfg.HTTPAddr)
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			if err := srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && err != http.ErrServerClosed {
				logger.Error("server TLS error", "error", err)
				os.Exit(1)
			}
		} else {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	// Start background workers
	bgCtx, cancelBg := context.WithCancel(context.Background())
	defer cancelBg()

	go tunnelHub.StartLivenessChecker(bgCtx)
	go allocator.StartExpiryTicker(bgCtx)
	go vncRelay.StartReaper(bgCtx)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("shutting down gateway", "signal", sig.String())

	cancelBg()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	// Close all device tunnels
	tunnelHub.CloseAll()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("gateway stopped")
}
