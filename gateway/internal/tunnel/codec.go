// Frame codec: JSON marshal/unmarshal, envelope validation,
// max-size enforcement (1 MiB), and type dispatch constants.
package tunnel

import (
	"encoding/json"
	"fmt"

	"github.com/iagent/gateway/internal/model"
)

// EncodeFrame marshals a frame to JSON bytes.
func EncodeFrame(frame model.Frame) ([]byte, error) {
	data, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("encode frame: %w", err)
	}
	if len(data) > model.FrameMaxSize {
		return nil, fmt.Errorf("frame exceeds max size: %d > %d", len(data), model.FrameMaxSize)
	}
	return data, nil
}

// DecodeFrame unmarshals JSON bytes into a frame, validating the envelope.
func DecodeFrame(data []byte) (model.Frame, error) {
	if len(data) > model.FrameMaxSize {
		return model.Frame{}, fmt.Errorf("frame exceeds max size: %d > %d", len(data), model.FrameMaxSize)
	}

	var frame model.Frame
	if err := json.Unmarshal(data, &frame); err != nil {
		return model.Frame{}, fmt.Errorf("decode frame: %w", err)
	}

	if err := ValidateEnvelope(frame); err != nil {
		return model.Frame{}, err
	}

	return frame, nil
}

// ValidateEnvelope checks required frame fields.
func ValidateEnvelope(frame model.Frame) error {
	if frame.Version != model.FrameVersion {
		return fmt.Errorf("unsupported frame version: %d", frame.Version)
	}
	if frame.Type == "" {
		return fmt.Errorf("frame type is required")
	}
	if frame.MsgID == "" {
		return fmt.Errorf("msg_id is required")
	}
	return nil
}

// NewFrame creates a new frame with common fields set.
func NewFrame(frameType model.FrameType, payload interface{}) (model.Frame, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return model.Frame{}, fmt.Errorf("marshal payload: %w", err)
	}

	return model.Frame{
		Version: model.FrameVersion,
		Type:    frameType,
		MsgID:   model.NewUUID().String(),
		Payload: payloadBytes,
	}, nil
}

// NewFrameWithTS creates a new frame with a provided timestamp.
func NewFrameWithTS(frameType model.FrameType, payload interface{}, ts int64) (model.Frame, error) {
	frame, err := NewFrame(frameType, payload)
	if err != nil {
		return frame, err
	}
	frame.TS = ts
	return frame, nil
}
