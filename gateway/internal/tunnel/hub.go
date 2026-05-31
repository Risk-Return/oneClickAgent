// Package tunnel implements the central device registry (Hub),
// managing online DeviceConns, superseding stale connections,
// and marking devices OFFLINE on heartbeat failure.
package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
)

// Hub is the central device registry and tunnel manager.
type Hub struct {
	mu             sync.RWMutex
	devices        map[model.UUID]*DeviceConn
	pending        map[string]*PendingAck

	// Handlers for incoming frames
	onJobProgress func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error
	onJobResult   func(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error
	onJobAccepted func(ctx context.Context, deviceID model.UUID, jobID model.UUID) error
	onJobRejected func(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error
	onAgentStatus func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error
	onStateSync   func(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error
	onSkillState  func(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error
	onFileAck     func(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error

	heartbeatInterval     time.Duration
	heartbeatMissThreshold time.Duration
	logger               *slog.Logger
}

// HubConfig holds configuration for the tunnel hub.
type HubConfig struct {
	HeartbeatInterval      time.Duration
	HeartbeatMissThreshold time.Duration
	OnJobProgress          func(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error
	OnJobResult            func(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error
	OnJobAccepted          func(ctx context.Context, deviceID model.UUID, jobID model.UUID) error
	OnJobRejected          func(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error
	OnAgentStatus          func(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error
	OnStateSync            func(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error
	OnSkillState           func(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error
	OnFileAck              func(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error
}

// NewHub creates a new tunnel Hub.
func NewHub(cfg HubConfig) *Hub {
	return &Hub{
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
		heartbeatInterval:      cfg.HeartbeatInterval,
		heartbeatMissThreshold: cfg.HeartbeatMissThreshold,
		logger:                 obs.Logger("tunnel"),
	}
}

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
}

// Register adds a new device connection, superseding any existing one.
func (h *Hub) Register(conn *DeviceConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.devices[conn.DeviceID()]; ok {
		h.logger.Warn("superseding existing device connection",
			"device_id", conn.DeviceID(),
		)
		existing.Close(4002, "superseded")
	}

	conn.SetHub(h)
	h.devices[conn.DeviceID()] = conn

	h.logger.Info("device registered",
		"device_id", conn.DeviceID(),
	)
}

// Unregister removes a device connection.
func (h *Hub) Unregister(deviceID model.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.devices, deviceID)
	h.logger.Info("device unregistered", "device_id", deviceID)
}

// GetConnection returns the connection for a device, or nil if offline.
func (h *Hub) GetConnection(deviceID model.UUID) *DeviceConn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.devices[deviceID]
}

// IsOnline checks if a device is currently connected.
func (h *Hub) IsOnline(deviceID model.UUID) bool {
	return h.GetConnection(deviceID) != nil
}

// SendFrame routes a frame to a specific device.
func (h *Hub) SendFrame(deviceID model.UUID, frame model.Frame) error {
	h.mu.RLock()
	conn := h.devices[deviceID]
	h.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("device %s is offline", deviceID)
	}

	select {
	case conn.outbound <- frame:
		return nil
	default:
		return fmt.Errorf("device %s outbound queue full", deviceID)
	}
}

// OnlineCount returns the number of currently connected devices.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.devices)
}

// HandleJobProgress is called by the device read pump.
func (h *Hub) HandleJobProgress(ctx context.Context, deviceID model.UUID, payload model.JobProgressPayload) error {
	if h.onJobProgress != nil {
		return h.onJobProgress(ctx, deviceID, payload)
	}
	return nil
}

// HandleJobResult is called by the device read pump.
func (h *Hub) HandleJobResult(ctx context.Context, deviceID model.UUID, payload model.JobResultPayload) error {
	if h.onJobResult != nil {
		return h.onJobResult(ctx, deviceID, payload)
	}
	return nil
}

// HandleJobAccepted is called by the device read pump.
func (h *Hub) HandleJobAccepted(ctx context.Context, deviceID model.UUID, jobID model.UUID) error {
	if h.onJobAccepted != nil {
		return h.onJobAccepted(ctx, deviceID, jobID)
	}
	return nil
}

// HandleJobRejected is called by the device read pump.
func (h *Hub) HandleJobRejected(ctx context.Context, deviceID model.UUID, payload model.JobRejectedPayload) error {
	if h.onJobRejected != nil {
		return h.onJobRejected(ctx, deviceID, payload)
	}
	return nil
}

// HandleAgentStatus is called by the device read pump.
func (h *Hub) HandleAgentStatus(ctx context.Context, deviceID model.UUID, payload model.AgentStatusPayload) error {
	if h.onAgentStatus != nil {
		return h.onAgentStatus(ctx, deviceID, payload)
	}
	return nil
}

// HandleStateSync is called by the device read pump.
func (h *Hub) HandleStateSync(ctx context.Context, deviceID model.UUID, payload model.StateSyncPayload) error {
	if h.onStateSync != nil {
		return h.onStateSync(ctx, deviceID, payload)
	}
	return nil
}

// HandleSkillState is called by the device read pump.
func (h *Hub) HandleSkillState(ctx context.Context, deviceID model.UUID, payload model.SkillStatePayload) error {
	if h.onSkillState != nil {
		return h.onSkillState(ctx, deviceID, payload)
	}
	return nil
}

// HandleFileAck is called by the device read pump.
func (h *Hub) HandleFileAck(ctx context.Context, deviceID model.UUID, payload model.FileAckPayload) error {
	if h.onFileAck != nil {
		return h.onFileAck(ctx, deviceID, payload)
	}
	return nil
}

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
			h.checkLiveness()
		}
	}
}

func (h *Hub) checkLiveness() {
	now := time.Now().Unix()
	threshold := now - int64(h.heartbeatMissThreshold.Seconds())

	h.mu.RLock()
	var expired []*DeviceConn
	for _, conn := range h.devices {
		if conn.LastSeen() < threshold {
			expired = append(expired, conn)
		}
	}
	h.mu.RUnlock()

	for _, conn := range expired {
		h.logger.Warn("device heartbeat missed, marking offline",
			"device_id", conn.DeviceID(),
			"last_seen", time.Unix(conn.LastSeen(), 0),
		)
		conn.Close(4001, "heartbeat timeout")
	}
}

// PendingAck tracks a frame waiting for acknowledgement.
type PendingAck struct {
	MsgID     string
	SentAt    time.Time
	Retries   int
	MaxRetries int
	Frame     model.Frame
}

// TrackPending registers a frame for ack tracking.
func (h *Hub) TrackPending(ack *PendingAck) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[ack.MsgID] = ack
}

// RemovePending removes a tracked ack on receipt.
func (h *Hub) RemovePending(msgID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.pending, msgID)
}

// PendingCount returns the number of unacknowledged frames.
func (h *Hub) PendingCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.pending)
}

// CloseAll closes all device connections gracefully.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, conn := range h.devices {
		conn.Close(1001, "gateway shutdown")
	}
	h.devices = make(map[model.UUID]*DeviceConn)
}

var _ = atomic.AddInt32
