// Per-device connection state: read pump, write pump with outbound queue,
// ack tracker with retransmit, last-seen timestamp, liveness checks,
// HELLO timeout, and msg_id dedup for idempotency.
package tunnel

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
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
	helloReceived atomic.Bool

	processed   sync.Map // map[string]bool — dedup by msg_id for idempotency
	frameCount  atomic.Int64
	frameLastTS atomic.Int64 // unix seconds of last frame rate reset

	tokenHash      string             // token hash at connection time
	tokenVerifier  TokenVerifier      // periodic token revocation check
}

// TokenVerifier checks whether a device token is still valid.
// Returns true if the token is valid (unchanged since connection).
type TokenVerifier func(ctx context.Context, deviceID model.UUID, tokenHash string) bool

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
	conn.frameLastTS.Store(time.Now().Unix())
	return conn
}

// SetTokenHash records the token hash used to authenticate this connection.
func (c *DeviceConn) SetTokenHash(hash string) { c.tokenHash = hash }

// SetTokenVerifier sets a callback to periodically verify token validity.
func (c *DeviceConn) SetTokenVerifier(v TokenVerifier) { c.tokenVerifier = v }

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
// Enforces HELLO timeout (10s from upgrade) per spec §1,
// and closes with 4004 on protocol violations (§6).
func (c *DeviceConn) StartReadPump(ctx context.Context) {
	defer c.Close(1000, "read pump done")

	// WebSocket-level ping/pong as secondary liveness check (§3).
	c.ws.SetPongHandler(func(appData string) error {
		c.touch()
		return nil
	})

	// HELLO timeout goroutine: close with 4003 if HELLO not received within 10s.
	helloCh := make(chan struct{}, 1)
	go func() {
		timer := time.NewTimer(model.HelloTimeout)
		defer timer.Stop()
		select {
		case <-helloCh:
			return
		case <-timer.C:
			c.logger.Warn("HELLO timeout, closing connection")
			c.Close(4003, "HELLO timeout")
			return
		case <-ctx.Done():
			return
		case <-c.done:
			return
		}
	}()

	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if !c.closed.Load() {
				c.logger.Error("read error", "error", err)
			}
			return
		}

		c.touch()

		// Protocol violation check: frame too large (§2).
		if len(msg) > model.FrameMaxSize {
			c.logger.Error("frame too large, closing", "size", len(msg))
			c.Close(4004, "protocol violation: frame exceeds max size")
			return
		}

		var frame model.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			c.logger.Error("frame decode error, closing", "error", err)
			c.Close(4004, "protocol violation: malformed frame")
			return
		}

		if frame.Version != model.FrameVersion {
			c.logger.Error("unsupported frame version, closing", "version", frame.Version)
			c.Close(4004, "protocol violation: unsupported version")
			return
		}

		// Rate limiting: cap frames per second, close with 4290 on overflow (§6).
		if !c.checkRateLimit() {
			c.Close(4290, "rate limited: too many frames per second")
			return
		}

		if frame.Type == model.FrameHello && !c.helloReceived.Load() {
			c.helloReceived.Store(true)
			select {
			case helloCh <- struct{}{}:
			default:
			}
			c.handleHello(ctx, &frame)
		}

		if err := c.handleFrame(ctx, frame); err != nil {
			c.logger.Error("frame handle error", "error", err, "type", frame.Type)
		}
	}
}

func (c *DeviceConn) handleHello(ctx context.Context, frame *model.Frame) {
	var payload model.HelloPayload
	if err := json.Unmarshal(frame.Payload, &payload); err != nil {
		c.logger.Error("hello payload decode error", "error", err)
		return
	}
	c.helloReceived.Store(true)
	c.logger.Info("received HELLO", "agents", payload.AgentCount)

	ack := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameHelloAck,
		MsgID:   model.NewUUID().String(),
		TS:      time.Now().UnixMilli(),
	}
	ackPayload := model.HelloAckPayload{
		ServerTime: time.Now().UnixMilli(),
		SessionID:  model.NewUUID().String(),
		Config: model.HelloAckConfig{
			HeartbeatS:    15,
			MaxFrameBytes: model.FrameMaxSize,
		},
	}
	ackBytes, _ := json.Marshal(ackPayload)
	ack.Payload = ackBytes

	select {
	case c.outbound <- ack:
	default:
		c.logger.Warn("outbound queue full, dropping HELLO_ACK")
	}
}

// StartWritePump drains the outbound channel and writes frames.
func (c *DeviceConn) StartWritePump(ctx context.Context) {
	defer c.Close(1000, "write pump done")

	// WS-level ping ticker (secondary liveness per spec §3 note).
	wsPingTicker := time.NewTicker(15 * time.Second)
	defer wsPingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-wsPingTicker.C:
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
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
	// Idempotency: skip already-processed msg_ids (spec §2).
	if frame.Type != model.FrameAck && frame.Type != model.FramePing && frame.Type != model.FramePong {
		if _, loaded := c.processed.LoadOrStore(frame.MsgID, true); loaded {
			c.logger.Debug("duplicate frame, acking only", "msg_id", frame.MsgID)
			c.sendAck(frame.MsgID)
			return nil
		}
	}

	switch frame.Type {
	case model.FrameHello:
		c.sendAck(frame.MsgID)
		return nil

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

	case model.FrameVNCOpened:
		var payload model.VNCOpenedPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleVNCOpened(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameCredPushAck:
		var payload model.CredPushAckPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleCredPushAck(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameCredCapture:
		var payload model.CredCapturePayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleCredCapture(ctx, c.deviceID, payload)
		}
		return nil

	case model.FrameVNCClose:
		if c.hub != nil {
			return c.hub.HandleVNCClose(ctx, c.deviceID, frame.Payload)
		}
		return nil

	case model.FrameSkillDispatchAck:
		if c.hub != nil {
			return c.hub.HandleSkillDispatchAck(ctx, c.deviceID, frame.Payload)
		}
		return nil

	case model.FrameFilePurged:
		if c.hub != nil {
			return c.hub.HandleFilePurged(ctx, c.deviceID, frame.Payload)
		}
		return nil

	case model.FrameCredCaptureAck:
		var payload model.CredCaptureAckPayload
		if err := json.Unmarshal(frame.Payload, &payload); err != nil {
			return err
		}
		if c.hub != nil {
			return c.hub.HandleCredCaptureAck(ctx, c.deviceID, payload)
		}
		return nil

	default:
		c.logger.Debug("unhandled frame type", "type", frame.Type)
		return nil
	}
}

func (c *DeviceConn) sendPong() error {
	if c.ws == nil {
		return nil
	}
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

func (c *DeviceConn) sendAck(msgID string) {
	if c.ws == nil {
		return
	}
	ackID := msgID
	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameAck,
		MsgID:   model.NewUUID().String(),
		AckID:   &ackID,
		TS:      time.Now().UnixMilli(),
	}
	data, err := json.Marshal(frame)
	if err != nil {
		c.logger.Error("ack encode error", "error", err)
		return
	}
	if err := c.ws.WriteMessage(websocket.TextMessage, data); err != nil {
		c.logger.Error("ack write error", "error", err)
	}
}

// Close shuts down the connection with a close code and reason.
func (c *DeviceConn) Close(code int, reason string) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}

	close(c.done)

	if c.ws != nil {
		msg := websocket.FormatCloseMessage(code, reason)
		c.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(5*time.Second))
		c.ws.Close()
	}

	if c.hub != nil {
		c.hub.Unregister(c.deviceID, c)
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

// checkRateLimit enforces a per-connection frame rate cap (100 frames/sec).
// Returns false if the cap is exceeded and the connection should be closed.
func (c *DeviceConn) checkRateLimit() bool {
	now := time.Now().Unix()
	last := c.frameLastTS.Load()

	if now != last {
		c.frameLastTS.Store(now)
		c.frameCount.Store(0)
		return true
	}

	count := c.frameCount.Add(1)
	return count <= 100
}

// StartTokenWatcher periodically verifies the device token has not been
// revoked (rotated) since the tunnel was established. If the token changed,
// closes with 4005 (§6).
func (c *DeviceConn) StartTokenWatcher(ctx context.Context) {
	if c.tokenVerifier == nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			if !c.tokenVerifier(ctx, c.deviceID, c.tokenHash) {
				c.logger.Warn("device token revoked, closing connection")
				c.Close(4005, "token revoked")
				return
			}
		}
	}
}

// AckTracker tracks frame acknowledgements with retransmit support.
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

// RetransmitReady returns frames ready for retransmit based on backoff schedule:
// 1s, 2s, 4s (doubling), max 3 retries. Marks frames that exceeded max retries.
func (t *AckTracker) RetransmitReady(now time.Time) (retry []*TrackedFrame, failed []*TrackedFrame) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, f := range t.frames {
		if f.Retries >= model.AckRetransmitMaxRetries {
			failed = append(failed, f)
			continue
		}

		backoff := model.AckRetransmitBase * (1 << f.Retries) // 1s, 2s, 4s
		if now.Sub(f.SentAt) >= backoff {
			f.Retries++
			f.SentAt = now
			retry = append(retry, f)
		}
	}
	return retry, failed
}

// RemoveFailed removes frames that exceeded max retries.
func (t *AckTracker) RemoveFailed(msgIDs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, id := range msgIDs {
		delete(t.frames, id)
	}
}

// StartRetransmitter runs a background goroutine that retransmits unacked frames
// with exponential backoff (1s, 2s, 4s, max 3 retries) per spec §2.
func (c *DeviceConn) StartRetransmitter(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			now := time.Now()
			retry, failed := c.acks.RetransmitReady(now)

			for _, f := range retry {
				c.logger.Debug("retransmitting unacked frame",
					"msg_id", f.MsgID,
					"retry", f.Retries,
				)
				select {
				case c.outbound <- f.Frame:
				default:
					c.logger.Warn("outbound queue full, dropping retransmit", "msg_id", f.MsgID)
				}
			}

			for _, f := range failed {
				c.logger.Error("frame exceeded max retransmit retries",
					"msg_id", f.MsgID,
				)
			}
			if len(failed) > 0 {
				var ids []string
				for _, f := range failed {
					ids = append(ids, f.MsgID)
				}
				c.acks.RemoveFailed(ids)
			}
		}
	}
}
