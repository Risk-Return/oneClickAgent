// Package mockdevice provides a reusable mock local device that speaks the
// iagent.tunnel.v1 protocol over WebSocket. It is designed for E2E testing the
// cloud gateway without requiring a real Python device or Docker agents.
package mockdevice

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/oneClickAgent/gateway/internal/model"
)

// BehaviorFunc is a callback invoked when the mock device receives a specific
// frame type. Return a response frame (or nil for no response). The raw payload
// bytes and the device's own agent list are provided.
type BehaviorFunc func(device *MockDevice, frame model.Frame) *model.Frame

// Config holds the mock device's identity and behavior configuration.
type Config struct {
	DeviceID       model.UUID
	DeviceToken    string
	GatewayURL     string
	AgentVersion   string
	Platform       string
	Resources      model.HelloResources
	Agents         []model.HelloAgent
	Subprotocol    string
	VerboseLogging bool
}

// MockDevice is a simulated local device that maintains a tunnel WebSocket to the
// cloud gateway. It handles HELLO/PING/ACK automatically. Test code registers
// custom BehaviorFunc callbacks and then calls Connect().
type MockDevice struct {
	cfg    Config
	conn   *websocket.Conn
	mu     sync.Mutex
	closed atomic.Bool

	logger *slog.Logger

	// handlers maps frame types → optional custom behavior.
	handlers map[model.FrameType]BehaviorFunc

	// Received frames are buffered here for test assertions.
	receivedFrames []model.Frame
	receivedMu     sync.Mutex

	// Pending ACKs (outbound frames we sent that need ACKing).
	pendingAcksMu sync.Mutex
	pendingAcks   map[string]chan struct{}

	done chan struct{}
}

const defaultHeartbeatInterval = 15 * time.Second

// New creates a MockDevice with defaults applied.
func New(cfg Config) *MockDevice {
	if cfg.Subprotocol == "" {
		cfg.Subprotocol = model.SubprotocolTunnel
	}
	if cfg.AgentVersion == "" {
		cfg.AgentVersion = "1.0.0"
	}
	if cfg.Platform == "" {
		cfg.Platform = "linux"
	}
	logger := slog.Default()
	if cfg.VerboseLogging {
		logger = slog.Default().With("device_id", cfg.DeviceID.String()[:8])
	}
	return &MockDevice{
		cfg:           cfg,
		logger:        logger,
		handlers:      make(map[model.FrameType]BehaviorFunc),
		pendingAcks:   make(map[string]chan struct{}),
		done:          make(chan struct{}),
	}
}

// On registers a custom handler for a frame type.
func (d *MockDevice) On(frameType model.FrameType, fn BehaviorFunc) {
	d.handlers[frameType] = fn
}

// Recv returns a copy of all frames received so far.
func (d *MockDevice) Recv() []model.Frame {
	d.receivedMu.Lock()
	defer d.receivedMu.Unlock()
	out := make([]model.Frame, len(d.receivedFrames))
	copy(out, d.receivedFrames)
	return out
}

// RecvByType returns received frames of a specific type.
func (d *MockDevice) RecvByType(ft model.FrameType) []model.Frame {
	d.receivedMu.Lock()
	defer d.receivedMu.Unlock()
	var out []model.Frame
	for _, f := range d.receivedFrames {
		if f.Type == ft {
			out = append(out, f)
		}
	}
	return out
}

// RecvLatestOf returns the most recent received frame of a given type, or nil.
func (d *MockDevice) RecvLatestOf(ft model.FrameType) *model.Frame {
	d.receivedMu.Lock()
	defer d.receivedMu.Unlock()
	for i := len(d.receivedFrames) - 1; i >= 0; i-- {
		if d.receivedFrames[i].Type == ft {
			f := d.receivedFrames[i]
			return &f
		}
	}
	return nil
}

// Connect dials the gateway tunnel endpoint, authenticates, and starts the
// read/write pumps. Blocks until HELLO_ACK is received or timeout.
func (d *MockDevice) Connect(ctx context.Context) error {
	u, err := url.Parse(d.cfg.GatewayURL)
	if err != nil {
		return fmt.Errorf("parse gateway URL: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "ws"
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+d.cfg.DeviceToken)
	header.Set("Sec-WebSocket-Protocol", d.cfg.Subprotocol)

	d.conn, _, err = websocket.DefaultDialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return fmt.Errorf("dial tunnel: %w", err)
	}

	helloAckCh := make(chan error, 1)
	d.On(model.FrameHelloAck, func(dev *MockDevice, f model.Frame) *model.Frame {
		helloAckCh <- nil
		return nil
	})

	// Send HELLO.
	if err := d.sendHello(); err != nil {
		return fmt.Errorf("send HELLO: %w", err)
	}

	// Wait for HELLO_ACK or timeout.
	select {
	case err := <-helloAckCh:
		if err != nil {
			return err
		}
	case <-time.After(10 * time.Second):
		return fmt.Errorf("HELLO_ACK timeout")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Start read pump.
	go d.readPump(ctx)
	// Start heartbeat pinger.
	go d.heartbeatLoop(ctx)

	return nil
}

// Close shuts down the connection.
func (d *MockDevice) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed.Swap(true) {
		return
	}
	select {
	case <-d.done:
	default:
		close(d.done)
	}
	if d.conn != nil {
		_ = d.conn.Close()
	}
}

func (d *MockDevice) sendHello() error {
	agents := d.cfg.Agents
	if agents == nil {
		agents = []model.HelloAgent{}
	}
	return d.sendFrame(model.FrameHello, model.HelloPayload{
		DeviceID:     d.cfg.DeviceID,
		AgentVersion: d.cfg.AgentVersion,
		Platform:     d.cfg.Platform,
		AgentCount:   len(agents),
		Agents:       agents,
		Resources:    d.cfg.Resources,
	})
}

func (d *MockDevice) sendFrame(frameType model.FrameType, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    frameType,
		MsgID:   model.NewUUID().String(),
		TS:      time.Now().UnixMilli(),
		Payload: payloadBytes,
	}

	d.trackPending(frame.MsgID)

	data, err := json.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	return d.write(data)
}

func (d *MockDevice) write(data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed.Load() {
		return fmt.Errorf("mockdevice: closed")
	}
	return d.conn.WriteMessage(websocket.TextMessage, data)
}

func (d *MockDevice) sendACK(msgID string) error {
	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameAck,
		MsgID:   model.NewUUID().String(),
		AckID:   &msgID,
		TS:      time.Now().UnixMilli(),
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return d.write(data)
}

func (d *MockDevice) trackPending(msgID string) {
	ch := make(chan struct{}, 1)
	d.pendingAcksMu.Lock()
	d.pendingAcks[msgID] = ch
	d.pendingAcksMu.Unlock()
}

func (d *MockDevice) ackReceived(msgID string) {
	d.pendingAcksMu.Lock()
	ch, ok := d.pendingAcks[msgID]
	if ok {
		ch <- struct{}{}
		delete(d.pendingAcks, msgID)
	}
	d.pendingAcksMu.Unlock()
}

func (d *MockDevice) readPump(ctx context.Context) {
	defer d.Close()
	for {
		select {
		case <-d.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		_, msg, err := d.conn.ReadMessage()
		if err != nil {
			if !d.closed.Load() {
				d.logger.Error("read error", "error", err)
			}
			return
		}

		var frame model.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			d.logger.Error("decode error", "error", err)
			continue
		}

		d.receivedMu.Lock()
		d.receivedFrames = append(d.receivedFrames, frame)
		d.receivedMu.Unlock()

		d.handleFrame(ctx, frame)
	}
}

func (d *MockDevice) handleFrame(ctx context.Context, frame model.Frame) {
	// Always ACK non-ACK frames.
	if frame.Type != model.FrameAck {
		_ = d.sendACK(frame.MsgID)
	}

	switch frame.Type {
	case model.FrameAck:
		if frame.AckID != nil {
			d.ackReceived(*frame.AckID)
		}
		return

	case model.FramePing:
		_ = d.sendFrame(model.FramePong, nil)
		return
	case model.FramePong:
		return
	case model.FrameHelloAck:
		// Handled via Connect() promise.
	default:
	}

	// Dispatch to custom handler.
	if fn, ok := d.handlers[frame.Type]; ok {
		resp := fn(d, frame)
		if resp != nil {
			_ = d.writeFrame(*resp)
		}
	}
}

func (d *MockDevice) writeFrame(frame model.Frame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return d.write(data)
}

func (d *MockDevice) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(defaultHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-d.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = d.sendFrame(model.FramePing, nil)
		}
	}
}

// Response helpers ─ test code uses these in BehaviorFunc callbacks.

func NewFrame(frameType model.FrameType, payload interface{}) model.Frame {
	b, _ := json.Marshal(payload)
	return model.Frame{
		Version: model.FrameVersion,
		Type:    frameType,
		MsgID:   model.NewUUID().String(),
		TS:      time.Now().UnixMilli(),
		Payload: b,
	}
}

// Send sends an arbitrary frame over the tunnel (device → gateway).
func (d *MockDevice) Send(frameType model.FrameType, payload interface{}) error {
	return d.sendFrame(frameType, payload)
}
