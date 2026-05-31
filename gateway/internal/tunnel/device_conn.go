// Per-device connection state: read pump, write pump with outbound queue,
// ack tracker with retransmit, last-seen timestamp, and liveness checks.
package tunnel

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iagent/gateway/internal/model"
	"github.com/iagent/gateway/internal/obs"
)

// DeviceConn represents a live tunnel connection to a device.
type DeviceConn struct {
	deviceID model.UUID
	ws       *websocket.Conn
	hub      *Hub
	outbound chan model.Frame
	acks     *AckTracker
	lastSeen atomic.Int64
	logger   *slog.Logger
	mu       sync.Mutex
	closed   atomic.Bool
	done     chan struct{}
}

// NewDeviceConn creates a new device connection wrapper.
func NewDeviceConn(deviceID model.UUID, ws *websocket.Conn) *DeviceConn {
	conn := &DeviceConn{
		deviceID: deviceID,
		ws:       ws,
		outbound: make(chan model.Frame, 256),
		acks:     NewAckTracker(),
		done:     make(chan struct{}),
		logger:   obs.Logger("tunnel.conn").With("device_id", deviceID),
	}
	conn.lastSeen.Store(time.Now().Unix())
	return conn
}

// DeviceID returns the device identifier.
func (c *DeviceConn) DeviceID() model.UUID {
	return c.deviceID
}

// SetHub associates this connection with a hub.
func (c *DeviceConn) SetHub(h *Hub) {
	c.hub = h
}

// LastSeen returns the unix timestamp of last activity.
func (c *DeviceConn) LastSeen() int64 {
	return c.lastSeen.Load()
}

func (c *DeviceConn) touch() {
	c.lastSeen.Store(time.Now().Unix())
}

// StartReadPump begins reading frames from the WebSocket.
func (c *DeviceConn) StartReadPump(ctx context.Context) {
	defer c.Close(1000, "read pump done")

	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if !c.closed.Load() {
				c.logger.Error("read error", "error", err)
			}
			return
		}

		c.touch()

		var frame model.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			c.logger.Error("frame decode error", "error", err)
			continue
		}

		if err := c.handleFrame(ctx, frame); err != nil {
			c.logger.Error("frame handle error", "error", err, "type", frame.Type)
		}
	}
}

// StartWritePump drains the outbound channel and writes frames.
func (c *DeviceConn) StartWritePump(ctx context.Context) {
	defer c.Close(1000, "write pump done")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case frame, ok := <-c.outbound:
			if !ok {
				return
			}

			data, err := json.Marshal(frame)
			if err != nil {
				c.logger.Error("frame encode error", "error", err)
				continue
			}

			if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
				c.logger.Error("write error", "error", err)
				return
			}
		}
	}
}

func (c *DeviceConn) handleFrame(ctx context.Context, frame model.Frame) error {
	switch frame.Type {
	case model.FramePing:
		return c.sendPong()

	case model.FramePong:
		c.touch()
		return nil

	case model.FrameAck:
		var ackID string
		if frame.AckID != nil {
			ackID = *frame.AckID
		}
		c.acks.Remove(ackID)
		if c.hub != nil {
			c.hub.RemovePending(ackID)
		}
		return nil

	case model.FrameJobProgress:
		var payload model.JobProgressPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleJobProgress(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameJobResult:
		var payload model.JobResultPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleJobResult(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameJobAccepted:
		var payload struct{ JobID model.UUID `json:"job_id"` }
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleJobAccepted(ctx, c.deviceID, payload.JobID)
		}
		return nil

	case model.FrameJobRejected:
		var payload model.JobRejectedPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleJobRejected(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameAgentStatus:
		var payload model.AgentStatusPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleAgentStatus(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameStateSync:
		var payload model.StateSyncPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleStateSync(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameSkillState:
		var payload model.SkillStatePayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleSkillState(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameFileAck:
		var payload model.FileAckPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleFileAck(ctx, c.deviceID, payload)
		}
		return nil

	default:
		c.logger.Debug("unhandled frame type", "type", frame.Type)
		return nil
	}
}

func (c *DeviceConn) sendPong() error {
	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FramePong,
		MsgID:   model.NewUUID().String(),
		TS:      time.Now().UnixMilli(),
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// Close shuts down the connection with a close code and reason.
func (c *DeviceConn) Close(code int, reason string) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}

	close(c.done)

	msg := websocket.FormatCloseMessage(code, reason)
	c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(5*time.Second))
	c.ws.Close()

	if c.hub != nil {
		c.hub.Unregister(c.deviceID)
	}

	c.logger.Info("device connection closed",
		"code", code,
		"reason", reason,
	)
}

// Send enqueues a frame for sending.
func (c *DeviceConn) Send(frame model.Frame) error {
	select {
	case c.outbound <- frame:
		return nil
	default:
		return errQueueFull
	}
}

var errQueueFull = &connError{"outbound queue full"}

type connError struct {
	msg string
}

func (e *connError) Error() string {
	return e.msg
}

// AckTracker tracks frame acknowledgements.
type AckTracker struct {
	mu     sync.Mutex
	frames map[string]*TrackedFrame
}

// TrackedFrame is a frame waiting for acknowledgement.
type TrackedFrame struct {
	MsgID     string
	Frame     model.Frame
	SentAt    time.Time
	Retries   int
}

// NewAckTracker creates a new ack tracker.
func NewAckTracker() *AckTracker {
	return &AckTracker{
		frames: make(map[string]*TrackedFrame),
	}
}

// Track adds a frame to be tracked.
func (t *AckTracker) Track(frame model.Frame) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.frames[frame.MsgID] = &TrackedFrame{
		MsgID:  frame.MsgID,
		Frame:  frame,
		SentAt: time.Now(),
	}
}

// Remove removes a tracked frame on ack.
func (t *AckTracker) Remove(msgID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.frames, msgID)
}

// Pending returns frames that have not been acked.
func (t *AckTracker) Pending() []*TrackedFrame {
	t.mu.Lock()
	defer t.mu.Unlock()
	var frames []*TrackedFrame
	for _, f := range t.frames {
		frames = append(frames, f)
	}
	return frames
}
