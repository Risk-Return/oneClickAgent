package tunnel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

func TestEncodeDecodeFrame(t *testing.T) {
	payload := map[string]string{"key": "value"}
	payloadBytes, _ := json.Marshal(payload)

	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FramePing,
		MsgID:   model.NewUUID().String(),
		TS:      time.Now().UnixMilli(),
		Payload: payloadBytes,
	}

	data, err := EncodeFrame(frame)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	decoded, err := DecodeFrame(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded.Type != model.FramePing {
		t.Errorf("expected PING, got %s", decoded.Type)
	}
	if decoded.MsgID != frame.MsgID {
		t.Errorf("msg_id mismatch")
	}
}

func TestDecodeFrameInvalid(t *testing.T) {
	_, err := DecodeFrame([]byte(`{"v":0,"type":"PING","msg_id":""}`))
	if err == nil {
		t.Error("should fail on invalid version")
	}

	_, err = DecodeFrame([]byte(`{"v":1,"type":"","msg_id":"x"}`))
	if err == nil {
		t.Error("should fail on empty type")
	}

	_, err = DecodeFrame([]byte(`{"v":1,"type":"PING"}`))
	if err == nil {
		t.Error("should fail on missing msg_id")
	}
}

func TestDecodeFrameTooLarge(t *testing.T) {
	large := make([]byte, model.FrameMaxSize+1)
	_, err := DecodeFrame(large)
	if err == nil {
		t.Error("should fail on oversize frame")
	}
}

func TestNewFrame(t *testing.T) {
	payload := map[string]string{"hello": "world"}
	frame, err := NewFrame(model.FrameHello, payload)
	if err != nil {
		t.Fatalf("new frame: %v", err)
	}
	if frame.Type != model.FrameHello {
		t.Errorf("expected HELLO, got %s", frame.Type)
	}
	if frame.MsgID == "" {
		t.Error("msg_id should be set")
	}
}

func TestNewFrameWithTS(t *testing.T) {
	ts := int64(1700000000000)
	frame, err := NewFrameWithTS(model.FramePing, nil, ts)
	if err != nil {
		t.Fatalf("new frame with ts: %v", err)
	}
	if frame.TS != ts {
		t.Errorf("expected ts %d, got %d", ts, frame.TS)
	}
}

func TestValidateEnvelope(t *testing.T) {
	valid := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FramePing,
		MsgID:   "test-1",
	}
	if err := ValidateEnvelope(valid); err != nil {
		t.Errorf("valid frame should pass: %v", err)
	}

	invalidVersion := model.Frame{Version: 99, Type: model.FramePing, MsgID: "x"}
	if err := ValidateEnvelope(invalidVersion); err == nil {
		t.Error("should fail on invalid version")
	}
}

func TestAckTracker(t *testing.T) {
	tracker := NewAckTracker()

	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameJobDispatch,
		MsgID:   "msg-001",
	}

	tracker.Track(frame)
	if len(tracker.Pending()) != 1 {
		t.Error("should have 1 pending frame")
	}

	tracker.Remove("msg-001")
	if len(tracker.Pending()) != 0 {
		t.Error("should have 0 pending frames after ack")
	}

	tracker.Remove("nonexistent") // should not panic
}

func TestRouterDispatch(t *testing.T) {
	router := NewRouter()
	called := false
	router.Register(model.FramePing, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		called = true
		return nil
	})
	_ = router
	_ = called
}

func TestAckTrackerRetransmit(t *testing.T) {
	tracker := NewAckTracker()

	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameJobDispatch,
		MsgID:   "msg-001",
	}

	tracker.Track(frame)

	// Immediately: no retransmit should be ready
	now := time.Now()
	retry, failed := tracker.RetransmitReady(now)
	if len(retry) != 0 {
		t.Error("should not retry immediately")
	}
	if len(failed) != 0 {
		t.Error("should not fail immediately")
	}

	// After 1.5s: first retry should be ready (1s backoff)
	retry, failed = tracker.RetransmitReady(now.Add(1500 * time.Millisecond))
	if len(retry) != 1 {
		t.Fatalf("expected 1 retry after 1.5s, got %d", len(retry))
	}
	if retry[0].Retries != 1 {
		t.Errorf("expected 1 retry, got %d", retry[0].Retries)
	}
	if len(failed) != 0 {
		t.Error("should not fail yet")
	}

	// After 2.5s more (total 4s): second retry (2s backoff from last send)
	retry, failed = tracker.RetransmitReady(now.Add(4 * time.Second))
	if len(retry) != 1 {
		t.Fatalf("expected 1 retry after 4s, got %d", len(retry))
	}
	if retry[0].Retries != 2 {
		t.Errorf("expected 2 retries, got %d", retry[0].Retries)
	}

	// After 6s more (total 10s): third retry (4s backoff from last send)
	retry, failed = tracker.RetransmitReady(now.Add(10 * time.Second))
	if len(retry) != 1 {
		t.Fatalf("expected 1 retry after 10s, got %d", len(retry))
	}
	if retry[0].Retries != 3 {
		t.Errorf("expected 3 retries, got %d", retry[0].Retries)
	}

	// After exceeding max retries: frame should appear in failed
	// Force 3 retries (max), then another retransmit check
	// Reset to 3 retries manually and check failure
	tracker.frames["msg-001"].Retries = model.AckRetransmitMaxRetries
	retry, failed = tracker.RetransmitReady(now.Add(20 * time.Second))
	if len(retry) != 0 {
		t.Error("should not retry after max retries")
	}
	if len(failed) != 1 {
		t.Fatalf("expected 1 failed, got %d", len(failed))
	}
	if failed[0].MsgID != "msg-001" {
		t.Errorf("expected msg-001 to fail, got %s", failed[0].MsgID)
	}

	// RemoveFailed cleans up
	tracker.RemoveFailed([]string{"msg-001"})
	if len(tracker.Pending()) != 0 {
		t.Error("should have 0 pending after RemoveFailed")
	}
}

func TestHubSupersede(t *testing.T) {
	hub := NewHub(HubConfig{})

	deviceID := model.NewUUID()

	conn1 := NewDeviceConn(deviceID, nil)
	hub.Register(conn1)

	if hub.OnlineCount() != 1 {
		t.Errorf("expected 1 online, got %d", hub.OnlineCount())
	}

	// Register second connection with same deviceID: supersedes first
	conn2 := NewDeviceConn(deviceID, nil)
	hub.Register(conn2)

	// conn1 should be closed
	if !conn1.closed.Load() {
		t.Error("conn1 should be closed after supersede")
	}

	if hub.OnlineCount() != 1 {
		t.Errorf("expected 1 online after supersede, got %d", hub.OnlineCount())
	}

	// conn2 should be registered
	if hub.GetConnection(deviceID) != conn2 {
		t.Error("conn2 should be the registered connection")
	}
}

func TestRateLimit(t *testing.T) {
	conn := NewDeviceConn(model.NewUUID(), nil)

	// First 100 frames should pass
	for i := 0; i < 100; i++ {
		if !conn.checkRateLimit() {
			t.Errorf("frame %d should pass rate limit", i)
		}
	}

	// 101st frame should be rejected
	if conn.checkRateLimit() {
		t.Error("101st frame should be rate limited")
	}

	// Reset on new second: manipulate frameLastTS
	conn.frameLastTS.Store(0) // force new second
	if !conn.checkRateLimit() {
		t.Error("should pass after second boundary")
	}
}

func TestIdempotency(t *testing.T) {
	deviceID := model.NewUUID()
	hub := NewHub(HubConfig{
		OnStateSync: func(ctx context.Context, dID model.UUID, payload model.StateSyncPayload) error {
			return nil
		},
	})

	conn := NewDeviceConn(deviceID, nil)
	conn.SetHub(hub)

	msgID := "dup-msg-001"
	payload, _ := json.Marshal(model.StateSyncPayload{
		Jobs:   []model.StateSyncJob{},
		Agents: []model.StateSyncAgent{},
	})

	frame := model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameStateSync,
		MsgID:   msgID,
		Payload: payload,
	}

	// First call should process (store in processed map)
	err := conn.handleFrame(context.Background(), frame)
	if err != nil {
		t.Errorf("first handle should succeed: %v", err)
	}
	if _, loaded := conn.processed.Load(msgID); !loaded {
		t.Error("msg_id should be in processed map")
	}

	// Second call with same msg_id should be skipped (but not error)
	err = conn.handleFrame(context.Background(), frame)
	if err != nil {
		t.Errorf("duplicate handle should not error: %v", err)
	}
}

func TestHandleHelloAckPayload(t *testing.T) {
	conn := NewDeviceConn(model.NewUUID(), nil)

	helloPayload := model.HelloPayload{
		DeviceID:     model.NewUUID(),
		AgentVersion: "1.0",
		Platform:     "linux",
		AgentCount:   2,
		Agents: []model.HelloAgent{
			{AgentID: model.NewUUID(), Status: model.AgentIdle, Port: 8090, Tags: []string{"opencode"}},
		},
		Resources: model.HelloResources{
			CPUCount: 4,
			MemoryMB: 8192,
			DiskMB:   102400,
		},
	}
	payload, _ := json.Marshal(helloPayload)

	frame := &model.Frame{
		Version: model.FrameVersion,
		Type:    model.FrameHello,
		MsgID:   model.NewUUID().String(),
		Payload: payload,
	}

	conn.handleHello(context.Background(), frame)

	if !conn.helloReceived.Load() {
		t.Error("helloReceived should be true after handleHello")
	}
}
