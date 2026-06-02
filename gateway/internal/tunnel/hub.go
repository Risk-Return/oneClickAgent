// Package tunnel implements the central device registry (Hub),
// managing online DeviceConns, superseding stale connections,
// and marking devices OFFLINE on heartbeat failure.
//
// # Multi-instance support
//
// The Hub uses a Registry interface to track which gateway instance owns
// each device's tunnel. In single-instance mode (default), InMemoryRegistry
// keeps everything local. For multi-instance, swap in a RedisRegistry that
// shares routing state across gateway nodes. DeviceConn objects themselves
// are always local (WebSocket connections are bound to a single process).
package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
)

// ─── Registry Interface ─────────────────────────────────────

// Registry tracks which gateway node owns each device's tunnel.
// DeviceConn instances are local to each node; the registry tells
// us where to route outbound frames.
type Registry interface {
	// Register records that a device is connected to this node.
	Register(ctx context.Context, deviceID model.UUID, nodeID string) error
	// Unregister removes a device from the registry.
	Unregister(ctx context.Context, deviceID model.UUID) error
	// GetNode returns the node ID that owns the device, or empty if offline.
	GetNode(ctx context.Context, deviceID model.UUID) (string, error)
	// IsOnline checks if a device is registered.
	IsOnline(ctx context.Context, deviceID model.UUID) bool
	// Count returns the number of registered devices.
	Count(ctx context.Context) int
	// List returns all registered device IDs.
	List(ctx context.Context) []model.UUID
	// Touch updates the last-seen timestamp.
	Touch(ctx context.Context, deviceID model.UUID, ts int64)
	// GetStale returns devices with last-seen before the given threshold.
	GetStale(ctx context.Context, threshold int64) []model.UUID
	// Close releases any resources held by the registry.
	Close() error
}

// ─── Hub ────────────────────────────────────────────────────

// Hub is the central device tunnel manager.
type Hub struct {
	registry Registry
	nodeID   string

	mu      sync.RWMutex
	devices map[model.UUID]*DeviceConn // local WebSocket connections
	pending map[string]*PendingAck

	// Handlers for incoming frames
	onJobProgress func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error
	onJobResult   func(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error
	onJobAccepted func(ctx context.Context, deviceID model.UUID, jobID model.UUID) error
	onJobRejected func(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error
	onAgentStatus func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error
	onStateSync   func(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error
	onSkillState  func(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error
	onFileAck     func(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error
	onVNCOpened   func(ctx context.Context, deviceID model.UUID, payload model.VNCOpenedPayload) error
	onCredPushAck    func(ctx context.Context, deviceID model.UUID, payload model.CredPushAckPayload) error
	onCredCaptureAck func(ctx context.Context, deviceID model.UUID, payload model.CredCaptureAckPayload) error

	heartbeatInterval      time.Duration
	heartbeatMissThreshold time.Duration
	logger                 *slog.Logger
}

// HubConfig holds configuration for the tunnel hub.
type HubConfig struct {
	NodeID                  string
	Registry                Registry
	HeartbeatInterval       time.Duration
	HeartbeatMissThreshold  time.Duration
	OnJobProgress           func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error
	OnJobResult             func(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error
	OnJobAccepted           func(ctx context.Context, deviceID model.UUID, jobID model.UUID) error
	OnJobRejected           func(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error
	OnAgentStatus           func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error
	OnStateSync             func(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error
	OnSkillState            func(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error
	OnFileAck               func(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error
	OnVNCOpened             func(ctx context.Context, deviceID model.UUID, payload model.VNCOpenedPayload) error
	OnCredPushAck           func(ctx context.Context, deviceID model.UUID, payload model.CredPushAckPayload) error
	OnCredCaptureAck        func(ctx context.Context, deviceID model.UUID, payload model.CredCaptureAckPayload) error
}

// NewHub creates a new tunnel Hub.
func NewHub(cfg HubConfig) *Hub {
	registry := cfg.Registry
	if registry == nil {
		registry = NewInMemoryRegistry()
	}
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = "node-" + model.NewUUID().String()[:8]
	}

	return &Hub{
		registry:               registry,
		nodeID:                 nodeID,
		devices:                make(map[model.UUID]*DeviceConn),
		pending:                make(map[string]*PendingAck),
		onJobProgress:          cfg.OnJobProgress,
		onJobResult:            cfg.OnJobResult,
		onJobAccepted:          cfg.OnJobAccepted,
		onJobRejected:          cfg.OnJobRejected,
		onAgentStatus:          cfg.OnAgentStatus,
		onStateSync:            cfg.OnStateSync,
		onSkillState:           cfg.OnSkillState,
		onFileAck:              cfg.OnFileAck,
		onVNCOpened:            cfg.OnVNCOpened,
		onCredPushAck:          cfg.OnCredPushAck,
		onCredCaptureAck:       cfg.OnCredCaptureAck,
		heartbeatInterval:      cfg.HeartbeatInterval,
		heartbeatMissThreshold: cfg.HeartbeatMissThreshold,
		logger:                 obs.Logger("tunnel"),
	}
}

// NodeID returns this instance's node identifier.
func (h *Hub) NodeID() string { return h.nodeID }

// SetHandlers sets handler callbacks on an existing Hub.
func (h *Hub) SetHandlers(cfg HubConfig) {
	h.onJobProgress = cfg.OnJobProgress
	h.onJobResult = cfg.OnJobResult
	h.onJobAccepted = cfg.OnJobAccepted
	h.onJobRejected = cfg.OnJobRejected
	h.onAgentStatus = cfg.OnAgentStatus
	h.onStateSync = cfg.OnStateSync
	h.onSkillState = cfg.OnSkillState
	h.onFileAck = cfg.OnFileAck
	h.onVNCOpened = cfg.OnVNCOpened
	h.onCredPushAck = cfg.OnCredPushAck
	h.onCredCaptureAck = cfg.OnCredCaptureAck
}

// Register adds a new device connection, superseding any existing one.
func (h *Hub) Register(conn *DeviceConn) {
	var supersede *DeviceConn

	h.mu.Lock()
	if existing, ok := h.devices[conn.DeviceID()]; ok {
		supersede = existing
	}
	conn.SetHub(h)
	h.devices[conn.DeviceID()] = conn
	h.mu.Unlock()

	if supersede != nil {
		h.logger.Warn("superseding existing device connection",
			"device_id", conn.DeviceID(),
		)
		supersede.Close(4002, "superseded")
	}

	_ = h.registry.Register(context.Background(), conn.DeviceID(), h.nodeID)

	go conn.StartRetransmitter(context.Background())

	h.logger.Info("device registered", "device_id", conn.DeviceID(), "node", h.nodeID)
}

// Unregister removes a device connection only if it matches the given conn.
func (h *Hub) Unregister(deviceID model.UUID, conn *DeviceConn) {
	h.mu.Lock()
	if existing, ok := h.devices[deviceID]; ok && existing == conn {
		delete(h.devices, deviceID)
	}
	h.mu.Unlock()

	_ = h.registry.Unregister(context.Background(), deviceID)
	h.logger.Info("device unregistered", "device_id", deviceID)
}

// GetConnection returns the local connection for a device, or nil.
func (h *Hub) GetConnection(deviceID model.UUID) *DeviceConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.devices[deviceID]
}

// IsOnline checks if a device is currently connected (local or remote).
func (h *Hub) IsOnline(deviceID model.UUID) bool {
	return h.GetConnection(deviceID) != nil
}

// SendFrame routes a frame to a specific device.
func (h *Hub) SendFrame(deviceID model.UUID, frame model.Frame) error {
	h.mu.RLock()
	conn := h.devices[deviceID]
	h.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("device %s is offline on this node", deviceID)
	}

	select {
	case conn.outbound <- frame:
		return nil
	default:
		return fmt.Errorf("device %s outbound queue full", deviceID)
	}
}

// OnlineCount returns the number of locally connected devices.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.devices)
}

// Touch updates the device heartbeat timestamp in the registry.
func (h *Hub) Touch(deviceID model.UUID) {
	h.registry.Touch(context.Background(), deviceID, time.Now().Unix())
}

// --- Frame handlers ---

func (h *Hub) HandleJobProgress(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error {
	if h.onJobProgress != nil {
		return h.onJobProgress(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleJobResult(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error {
	if h.onJobResult != nil {
		return h.onJobResult(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleJobAccepted(ctx context.Context, deviceID model.UUID, jobID model.UUID) error {
	if h.onJobAccepted != nil {
		return h.onJobAccepted(ctx, deviceID, jobID)
	}
	return nil
}

func (h *Hub) HandleJobRejected(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error {
	if h.onJobRejected != nil {
		return h.onJobRejected(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleAgentStatus(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error {
	if h.onAgentStatus != nil {
		return h.onAgentStatus(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleStateSync(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error {
	if h.onStateSync != nil {
		return h.onStateSync(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleSkillState(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error {
	if h.onSkillState != nil {
		return h.onSkillState(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleFileAck(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error {
	if h.onFileAck != nil {
		return h.onFileAck(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleVNCOpened(ctx context.Context, deviceID model.UUID, payload model.VNCOpenedPayload) error {
	if h.onVNCOpened != nil {
		return h.onVNCOpened(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleCredPushAck(ctx context.Context, deviceID model.UUID, payload model.CredPushAckPayload) error {
	if h.onCredPushAck != nil {
		return h.onCredPushAck(ctx, deviceID, payload)
	}
	return nil
}

func (h *Hub) HandleCredCaptureAck(ctx context.Context, deviceID model.UUID, payload model.CredCaptureAckPayload) error {
	if h.onCredCaptureAck != nil {
		return h.onCredCaptureAck(ctx, deviceID, payload)
	}
	return nil
}

// --- Liveness ---

// StartLivenessChecker runs a background goroutine that marks devices
// offline when they miss heartbeats.
func (h *Hub) StartLivenessChecker(ctx context.Context) {
	ticker := time.NewTicker(h.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.checkLiveness(ctx)
		}
	}
}

func (h *Hub) checkLiveness(ctx context.Context) {
	now := time.Now().Unix()
	threshold := now - int64(h.heartbeatMissThreshold.Seconds())

	stale := h.registry.GetStale(ctx, threshold)
	for _, deviceID := range stale {
		h.mu.RLock()
		conn := h.devices[deviceID]
		h.mu.RUnlock()

		if conn != nil {
			h.logger.Warn("device heartbeat missed, marking offline",
				"device_id", deviceID,
			)
			conn.Close(4001, "heartbeat timeout")
		}
	}
}

// --- Ack tracking ---

type PendingAck struct {
	MsgID      string
	SentAt     time.Time
	Retries    int
	MaxRetries int
	Frame      model.Frame
}

func (h *Hub) TrackPending(ack *PendingAck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[ack.MsgID] = ack
}

func (h *Hub) RemovePending(msgID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pending, msgID)
}

func (h *Hub) PendingCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.pending)
}

// --- Shutdown ---

func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, conn := range h.devices {
		conn.Close(1001, "gateway shutdown")
	}
	h.devices = make(map[model.UUID]*DeviceConn)
	_ = h.registry.Close()
}
