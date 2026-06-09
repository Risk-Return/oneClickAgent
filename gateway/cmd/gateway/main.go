// Main entry point for the oneClickAgent Cloud Gateway.
// Wires config, initialises store/tunnel hub/pubsub/HTTP server,
// and handles graceful shutdown (SIGINT/SIGTERM).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	allocator.SetDispatchDeps(files, func(ctx context.Context, jobID model.UUID) ([]model.UUID, error) {
		creds, err := credStore.ListByJob(ctx, jobID)
		if err != nil {
			return nil, err
		}
		ids := make([]model.UUID, 0, len(creds))
		for _, c := range creds {
			ids = append(ids, c.ID)
		}
		return ids, nil
	}, func(ctx context.Context, jobID, agentID, deviceID model.UUID) error {
		return httpapi.PushCredentialsForJob(ctx, jobID, agentID, deviceID, credStore, credVault, tunnelHub)
	})
	vncRelay := vncrelay.NewRelay(
		tunnelHub.NodeID(),
		int64(cfg.VNCSessionBufBytes),
		cfg.VNCMaxSessionsPerUser,
	)

	// Wire tunnel hub handlers
	tunnelHub.SetHandlers(tunnel.HubConfig{
		OnHello: func(ctx context.Context, deviceID model.UUID, payload model.HelloPayload) error {
			_ = devices.UpdateStatus(ctx, deviceID, model.DeviceOnline)
			if payload.Platform != "" {
				_ = devices.UpdatePlatform(ctx, deviceID, payload.Platform)
			}
			for _, a := range payload.Agents {
				if a.Name != "" {
					_ = agents.UpdateName(ctx, a.AgentID, a.Name)
				}
			}
			_ = allocator.ReconcilePool(ctx, deviceID, payload.Agents)
			return nil
		},
		OnJobProgress: func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error {
			if err := jobs.UpdateProgress(ctx, payload.JobID, payload.Percent, payload.Message, payload.Status); err != nil {
				return err
			}
			broker.Publish(pubsub.JobTopic(payload.JobID), model.WSEvent{
				Type:  model.WSEventJobProgress,
				Topic: pubsub.JobTopic(payload.JobID),
			})
			return nil
		},
		OnJobResult: func(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error {
			if err := jobs.UpdateResult(ctx, payload.JobID, payload.Status, payload.Result); err != nil {
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
		OnJobAccepted: func(ctx context.Context, deviceID model.UUID, jobID model.UUID) error {
			return jobs.UpdateStatus(ctx, jobID, model.JobRunning)
		},
		OnJobRejected: func(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error {
			_ = jobs.UpdateResult(ctx, payload.JobID, model.JobFailed, nil)
			if job, _ := jobs.GetByID(ctx, payload.JobID); job != nil && job.AgentID != nil {
				_ = allocator.Release(ctx, *job.AgentID)
			}
			broker.Publish(pubsub.JobTopic(payload.JobID), model.WSEvent{
				Type:  model.WSEventJobResult,
				Topic: pubsub.JobTopic(payload.JobID),
			})
			return nil
		},
		OnStateSync: func(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error {
			deviceJobs := make(map[model.UUID]bool)
			for _, j := range payload.Jobs {
				deviceJobs[j.JobID] = true
			}
			agents, _ := agents.ListByDevice(ctx, deviceID)
			for _, a := range agents {
				activeJobs, _ := jobs.ListByAgent(ctx, a.ID)
				for _, j := range activeJobs {
					if j.Status.IsActive() && !deviceJobs[j.ID] {
						_ = jobs.UpdateResult(ctx, j.ID, model.JobFailed, nil)
						_ = allocator.Release(ctx, a.ID)
					}
				}
			}
			return nil
		},
		OnAgentStatus: func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error {
			return agents.UpdateStatus(ctx, payload.AgentID, payload.Status)
		},
		OnSkillState: func(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error {
			return skillDispatch.UpdateDeviceSkillState(ctx, deviceID, payload)
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
		OnVNCClose: func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
			var p model.VNCOpenedPayload
			if err := json.Unmarshal(payload, &p); err != nil {
				return err
			}
			vncRelay.CloseSession(p.SessionID, "device closed session")
			return vncStore.Close(ctx, p.SessionID)
		},
		OnCredPushAck: func(ctx context.Context, deviceID model.UUID, payload model.CredPushAckPayload) error {
			if payload.Status == "INJECTED" {
				return credStore.Touch(ctx, payload.CredentialID)
			}
			slog.Error("credential push failed", "job_id", payload.JobID, "cred_id", payload.CredentialID, "error", payload.Error)
			return nil
		},
		OnCredCapture: func(ctx context.Context, deviceID model.UUID, payload model.CredCapturePayload) error {
			if !credVault.IsConfigured() {
				slog.Error("credential capture failed: vault not configured")
				return nil
			}
			data, err := base64.StdEncoding.DecodeString(payload.Data)
			if err != nil {
				slog.Error("credential capture: invalid base64 data", "error", err)
				return err
			}
			hasher := sha256.New()
			hasher.Write(data)
			if hex.EncodeToString(hasher.Sum(nil)) != payload.SHA256 {
				slog.Error("credential capture: sha256 mismatch")
				return fmt.Errorf("sha256 mismatch")
			}
			enc, err := credVault.Encrypt(data)
			if err != nil {
				slog.Error("credential capture: encrypt failed", "error", err)
				return err
			}
			job, _ := jobs.GetByID(ctx, payload.JobID)
			if job == nil {
				slog.Error("credential capture: job not found", "job_id", payload.JobID)
				return fmt.Errorf("job not found")
			}
			cred := &model.BrowserCredential{
				UserID:          job.UserID,
				Label:           payload.Label,
				Origin:          payload.Origin,
				StorageStateEnc: enc.StorageStateEnc,
				Nonce:           enc.Nonce,
				AuthTag:         enc.AuthTag,
				KeyID:           "default",
				SHA256:          enc.SHA256,
			}
			if err := credStore.Create(ctx, cred); err != nil {
				slog.Error("credential capture: store create failed", "error", err)
				return err
			}
			ackFrame, _ := tunnel.NewFrame(model.FrameCredCaptureAck, model.CredCaptureAckPayload{
				CredentialID: cred.ID,
				SessionID:    payload.SessionID,
				Status:       "STORED",
			})
			_ = tunnelHub.SendFrame(deviceID, ackFrame)
			return nil
		},
		OnCredCaptureAck: func(ctx context.Context, deviceID model.UUID, payload model.CredCaptureAckPayload) error {
			if payload.Status != "STORED" {
				slog.Error("credential capture ack failed", "session_id", payload.SessionID, "error", payload.Error)
			}
			return nil
		},
		OnSkillDispatchAck: func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
			slog.Info("skill dispatch ack received", "device_id", deviceID)
			return nil
		},
		OnFilePurged: func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
			return fileRelay.CleanupStagedFiles(ctx)
		},
		OnFilePullBegin: func(ctx context.Context, deviceID model.UUID, payload model.FilePullBeginPayload) error {
			return fileRelay.OnFilePullBegin(ctx, deviceID, payload)
		},
		OnFilePullChunk: func(ctx context.Context, deviceID model.UUID, payload model.FilePullChunkPayload) error {
			return fileRelay.OnFilePullChunk(ctx, deviceID, payload)
		},
		OnFilePullEnd: func(ctx context.Context, deviceID model.UUID, payload model.FilePullEndPayload) error {
			return fileRelay.OnFilePullEnd(ctx, deviceID, payload)
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
