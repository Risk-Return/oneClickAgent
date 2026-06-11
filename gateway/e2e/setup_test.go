// Package e2e_test provides end-to-end tests for the cloud gateway.
// These tests require a running PostgreSQL instance.
// Set ONE_CLICK_DSN to a writable test database:
//
//	go test -tags=e2e ./e2e/ -v
//
// The test database is cleaned between test runs (all tables truncated).
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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

// ─── TestDB helpers ───────────────────────────────────────────

func e2eDSN() string {
	if d := os.Getenv("ONE_CLICK_DSN"); d != "" {
		return d
	}
	return "postgresql://iagent:iagent_dev_password@localhost:5432/iagent_e2e?sslmode=disable"
}

// e2eDB returns a connected PostgreSQL pool and runs migrations.
func e2eDB(t *testing.T) *store.DB {
	t.Helper()
	ctx := context.Background()
	db, err := store.NewDB(ctx, e2eDSN())
	if err != nil {
		t.Skipf("skip e2e: no postgres available: %v (set ONE_CLICK_DSN)", err)
	}
	t.Cleanup(func() { db.Close() })

	truncateAll(t, db)
	return db
}

func truncateAll(t *testing.T, db *store.DB) {
	t.Helper()
	// Order matters: circular FKs (users↔organizations) require SET NULL first
	tables := []string{
		"job_credentials", "vnc_sessions", "browser_credentials",
		"job_files", "device_skills", "agent_skills", "skill_grants",
		"skill_versions", "skills",
		"files",
		"jobs", "agents", "devices",
		"refresh_tokens", "audit_log",
	}
	for _, tbl := range tables {
		_, err := db.Pool.Exec(context.Background(), "DELETE FROM "+tbl)
		if err != nil {
			t.Logf("truncate %s (may be ok): %v", tbl, err)
		}
	}
	// Handle circular FK: null org_id on users before deleting orgs
	_, _ = db.Pool.Exec(context.Background(), "UPDATE users SET org_id = NULL")
	_, _ = db.Pool.Exec(context.Background(), "DELETE FROM users")
	_, _ = db.Pool.Exec(context.Background(), "DELETE FROM organizations")
}

// ─── E2E Test Harness ─────────────────────────────────────────

// E2EHarness holds a fully wired gateway HTTP test server plus helpers.
type E2EHarness struct {
	DB        *store.DB
	Server    *httptest.Server
	BaseURL   string
	Router    *httpapi.Dependencies
	Hub       *tunnel.Hub
	Allocator *pool.Allocator
	Broker    *pubsub.Broker
}

// NewHarness creates a fully wired gateway for e2e testing.
func NewHarness(t *testing.T) *E2EHarness {
	t.Helper()

	db := e2eDB(t)
	cfg := config.Config{
		HTTPAddr:                ":0",
		JWTSecret:               "e2e-test-jwt-secret-that-is-at-least-32-chars!",
		AccessTTL:               15 * time.Minute,
		RefreshTTL:              720 * time.Hour,
		FileStore:               "local:" + t.TempDir(),
		MaxUploadBytes:          100 * 1024 * 1024,
		HeartbeatInterval:       15 * time.Second,
		HeartbeatMissThreshold:  45 * time.Second,
		QueueTTL:                1 * time.Hour,
		MaxQueuedPerUser:        10,
		RateLimitAPIPerSec:      10000,
		RateLimitAuthPerMin:     10000,
		RateLimitJobSubmitPerMin: 10000,
		VNCMaxSessionsPerUser:   2,
		VNCSessionBufBytes:      4 << 20,
		VNCIdleTTLSecs:          300,
		VNCMaxTTLSecs:           1800,
		ShutdownTimeout:         10 * time.Second,
		LogLevel:                "error",
		LogFormat:               "text",
		Env:                     "development",
		CORSAllowedOrigins:      []string{"*"},
	}

	_ = obs.InitLogger(cfg.LogLevel, cfg.LogFormat)

	// Stores
	users := store.NewUserStore(db)
	tokens := store.NewTokenStore(db)
	devices := store.NewDeviceStore(db)
	agents := store.NewAgentStore(db)
	jobs := store.NewJobStore(db)
	files := store.NewFileStore(db)
	skills := store.NewSkillStore(db)
	orgs := store.NewOrgStore(db)
	audit := store.NewAuditStore(db)
	vncStore := store.NewVNCSessionStore(db)
	credStore := store.NewCredentialStore(db)

	jwtManager := auth.NewJWTManager(cfg.JWTSecret, cfg.AccessTTL)
	hasher := auth.NewPasswordHasher()
	broker := pubsub.NewBroker()

	tunnelHub := tunnel.NewHub(tunnel.HubConfig{
		HeartbeatInterval:      cfg.HeartbeatInterval,
		HeartbeatMissThreshold: cfg.HeartbeatMissThreshold,
	})

	allocator := pool.NewAllocator(agents, jobs, tunnelHub, broker, cfg.QueueTTL, cfg.MaxQueuedPerUser)

	fileRelay := relay.NewFileRelay(files, tunnelHub, cfg.FileStore, cfg.MaxUploadBytes, 24*time.Hour)

	vault := skillvault.NewVault(skills, cfg.FileStore+"/skills")
	skillDispatch := skillvault.NewDispatcher(vault, skills, tunnelHub)
	skillDispatch.SetDeviceLister(func(ctx context.Context) ([]model.Device, error) {
		return devices.ListAll(ctx)
	})

	credVault, err := credvault.NewVault("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "")
	if err != nil {
		t.Fatalf("cred vault: %v", err)
	}

	vncRelay := vncrelay.NewRelay(tunnelHub.NodeID(), cfg.VNCSessionBufBytes, cfg.VNCMaxSessionsPerUser)

	tunnelHub.SetHandlers(tunnel.HubConfig{
		OnHello: func(ctx context.Context, deviceID model.UUID, payload model.HelloPayload) error {
			_ = devices.UpdateStatus(ctx, deviceID, model.DeviceOnline)
			return nil
		},
		OnJobProgress: func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error {
			_ = jobs.UpdateProgress(ctx, payload.JobID, payload.Percent, payload.Message, payload.Status)
			if payload.Status == model.JobRunning {
				_ = jobs.UpdateStatus(ctx, payload.JobID, payload.Status)
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
			_ = jobs.UpdateResult(ctx, payload.JobID, payload.Status, result)
			broker.Publish(pubsub.JobTopic(payload.JobID), model.WSEvent{
				Type:  model.WSEventJobResult,
				Topic: pubsub.JobTopic(payload.JobID),
			})
			job, err := jobs.GetByID(ctx, payload.JobID)
			if err == nil && job != nil && job.AgentID != nil {
				_ = allocator.Release(ctx, *job.AgentID)
			}
			return nil
		},
		OnAgentStatus: func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error {
			return agents.UpdateStatus(ctx, payload.AgentID, payload.Status)
		},
		OnStateSync: func(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error {
			for _, j := range payload.Jobs {
				_ = jobs.UpdateStatus(ctx, j.JobID, j.Status)
			}
			return nil
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
			vncRelay.CloseSession(payload.SessionID, "device error: "+payload.Error)
			return nil
		},
		OnCredPushAck: func(ctx context.Context, deviceID model.UUID, payload model.CredPushAckPayload) error {
			if payload.Status == "ok" {
				return credStore.Touch(ctx, payload.CredentialID)
			}
			return nil
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
		OnJobLoginRequired: func(ctx context.Context, deviceID model.UUID, payload model.JobLoginRequiredPayload) error {
			// No-op in e2e tests
			return nil
		},
		OnDisconnect: func(ctx context.Context, deviceID model.UUID) error {
			return nil
		},
	})

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

	router := httpapi.NewRouter(deps)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	h := &E2EHarness{
		DB:        db,
		Server:    ts,
		BaseURL:   ts.URL,
		Router:    deps,
		Hub:       tunnelHub,
		Allocator: allocator,
		Broker:    broker,
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go tunnelHub.StartLivenessChecker(bgCtx)
	go allocator.StartExpiryTicker(bgCtx)
	go vncRelay.StartReaper(bgCtx)

	return h
}

// ─── Convenience helpers ─────────────────────────────────────

func (h *E2EHarness) Post(t *testing.T, path string, body interface{}, token string) httpResp {
	return doRequest(t, h.BaseURL+path, "POST", body, token)
}

func (h *E2EHarness) Get(t *testing.T, path string, token string) httpResp {
	return doRequest(t, h.BaseURL+path, "GET", nil, token)
}

func (h *E2EHarness) Delete(t *testing.T, path string, token string) httpResp {
	return doRequest(t, h.BaseURL+path, "DELETE", nil, token)
}

func (h *E2EHarness) Patch(t *testing.T, path string, body interface{}, token string) httpResp {
	return doRequest(t, h.BaseURL+path, "PATCH", body, token)
}

func (h *E2EHarness) RegisterAndLogin(t *testing.T, email, username, password string) model.AuthResponse {
	t.Helper()
	resp := h.Post(t, "/api/v1/auth/register", model.RegisterRequest{
		Email:    email,
		Username: username,
		Password: password,
	}, "")
	return h.unmarshalAuth(t, resp)
}

func (h *E2EHarness) RegisterAdmin(t *testing.T) model.AuthResponse {
	t.Helper()
	resp := h.RegisterAndLogin(t, "admin-"+uniq()+"@e2e.test", "admin-"+uniq(), "AdminPass!12345")
	ctx := context.Background()
	u, err := h.Router.Users.GetByID(ctx, resp.User.ID)
	if err != nil || u == nil {
		t.Fatalf("get user for promotion: %v", err)
	}
	u.Role = model.RoleAdmin
	if err := h.Router.Users.Update(ctx, u); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}
	resp.User.Role = model.RoleAdmin
	token, _ := h.Router.JWT.IssueAccessToken(&resp.User)
	resp.AccessToken = token
	return resp
}

func (h *E2EHarness) RegisterCustomer(t *testing.T, tier model.UserTier) model.AuthResponse {
	t.Helper()
	resp := h.RegisterAndLogin(t, "cust-"+uniq()+"@e2e.test", "cust-"+uniq(), "CustomerPass!12345")
	if tier != model.TierFree {
		ctx := context.Background()
		_ = h.Router.Users.UpdateTier(ctx, resp.User.ID, tier)
	}
	return resp
}

func (h *E2EHarness) CreateDevice(t *testing.T, adminToken string) (model.Device, string) {
	t.Helper()
	createResp := h.Post(t, "/api/v1/devices", model.CreateDeviceRequest{
		Name:        "e2e-device-" + uniq(),
		Description: "e2e test device",
	}, adminToken)
	if createResp.StatusCode != 201 {
		t.Fatalf("create device: status=%d body=%s", createResp.StatusCode, createResp.Body)
	}
	var createBody model.CreateDeviceResponse
	if err := json.Unmarshal([]byte(createResp.Body), &createBody); err != nil {
		t.Fatalf("unmarshal create device: %v\nbody: %s", err, createResp.Body)
	}

	enrollResp := h.Post(t, "/api/v1/devices/enroll", model.EnrollmentRequest{
		EnrollmentCode: createBody.EnrollmentCode,
	}, "")
	if enrollResp.StatusCode != 200 {
		t.Fatalf("enroll device: status=%d body=%s", enrollResp.StatusCode, enrollResp.Body)
	}
	var enrollBody model.EnrollmentResponse
	if err := json.Unmarshal([]byte(enrollResp.Body), &enrollBody); err != nil {
		t.Fatalf("unmarshal enroll: %v\nbody: %s", err, enrollResp.Body)
	}
	return createBody.Device, enrollBody.DeviceToken
}

func (h *E2EHarness) unmarshalAuth(t *testing.T, resp httpResp) model.AuthResponse {
	t.Helper()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("auth request failed: status=%d body=%s", resp.StatusCode, resp.Body)
	}
	var ar model.AuthResponse
	if err := json.Unmarshal([]byte(resp.Body), &ar); err != nil {
		t.Fatalf("unmarshal auth: %v\nbody: %s", err, resp.Body)
	}
	return ar
}

func (h *E2EHarness) unmarshalJob(t *testing.T, resp httpResp) model.JobResponse {
	t.Helper()
	var jr model.JobResponse
	if err := json.Unmarshal([]byte(resp.Body), &jr); err != nil {
		t.Fatalf("unmarshal job: %v\nbody: %s", err, resp.Body)
	}
	return jr
}

func uniq() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
}

func (h *E2EHarness) TunnelURL() string {
	return "ws" + strings.TrimPrefix(h.BaseURL, "http") + "/tunnel"
}
