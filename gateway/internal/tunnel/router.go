// Inbound frame router: dispatches frames from device read pumps
// to registered handlers (job progress, agent status, state sync, skill state, file ack).
package tunnel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oneClickAgent/gateway/internal/model"
)

// FrameHandler is a function that handles a specific frame payload.
type FrameHandler func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error

// Router dispatches inbound tunnel frames to registered handlers.
type Router struct {
	handlers map[model.FrameType]FrameHandler
}

// NewRouter creates a new frame router.
func NewRouter() *Router {
	return &Router{
		handlers: make(map[model.FrameType]FrameHandler),
	}
}

// Register adds a handler for a specific frame type.
func (r *Router) Register(frameType model.FrameType, handler FrameHandler) {
	r.handlers[frameType] = handler
}

// Dispatch routes a frame to the registered handler.
func (r *Router) Dispatch(ctx context.Context, deviceID model.UUID, frame model.Frame) error {
	handler, ok := r.handlers[frame.Type]
	if !ok {
		return fmt.Errorf("no handler registered for frame type: %s", frame.Type)
	}
	return handler(ctx, deviceID, frame.Payload)
}

// RegisterAll registers common handlers from a Hub.
func (r *Router) RegisterAll(hub *Hub) {
	r.Register(model.FrameJobProgress, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.JobProgressPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleJobProgress(ctx, deviceID, p)
	})

	r.Register(model.FrameJobResult, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.JobResultPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleJobResult(ctx, deviceID, p)
	})

	r.Register(model.FrameJobAccepted, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p struct{ JobID model.UUID `json:"job_id"` }
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleJobAccepted(ctx, deviceID, p.JobID)
	})

	r.Register(model.FrameJobRejected, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.JobRejectedPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleJobRejected(ctx, deviceID, p)
	})

	r.Register(model.FrameAgentStatus, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.AgentStatusPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleAgentStatus(ctx, deviceID, p)
	})

	r.Register(model.FrameStateSync, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.StateSyncPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleStateSync(ctx, deviceID, p)
	})

	r.Register(model.FrameSkillState, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.SkillStatePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleSkillState(ctx, deviceID, p)
	})

	r.Register(model.FrameFileAck, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.FileAckPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleFileAck(ctx, deviceID, p)
	})

	r.Register(model.FrameVNCOpened, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.VNCOpenedPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleVNCOpened(ctx, deviceID, p)
	})

	r.Register(model.FrameCredPushAck, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.CredPushAckPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleCredPushAck(ctx, deviceID, p)
	})

	r.Register(model.FrameCredCaptureAck, func(ctx context.Context, deviceID model.UUID, payload json.RawMessage) error {
		var p model.CredCaptureAckPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		return hub.HandleCredCaptureAck(ctx, deviceID, p)
	})
}
