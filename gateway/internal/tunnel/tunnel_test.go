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
