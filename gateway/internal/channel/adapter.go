// Package channel defines the ChannelAdapter interface for multi-channel support.
// Web adapter is implemented; Feishu/QQ/etc. are registered as no-op stubs.
package channel

import (
	"context"

	"github.com/oneClickAgent/gateway/internal/model"
)

// Adapter defines the interface for a communication channel.
type Adapter interface {
	// Name returns the channel name (e.g., "web", "feishu", "qq").
	Name() string

	// ParseInbound normalizes an inbound message into a canonical command.
	ParseInbound(ctx context.Context, raw []byte) (*Command, error)

	// SendOutbound sends a response or event to the channel.
	SendOutbound(ctx context.Context, userID model.UUID, event interface{}) error

	// Authenticate authenticates a channel-specific request.
	// Returns the authenticated user ID.
	Authenticate(ctx context.Context, raw []byte) (*model.User, error)

	// IsEnabled returns whether the channel is active.
	IsEnabled() bool
}

// Command is a canonical representation of an inbound command from any channel.
type Command struct {
	UserID  model.UUID
	AgentID *model.UUID
	Text    string
	SkillID *model.UUID
	FileIDs []model.UUID
	Channel string
}

// StubAdapter is a no-op adapter for unimplemented channels.
type StubAdapter struct {
	name string
}

// NewStubAdapter creates a stub adapter for a channel.
func NewStubAdapter(name string) *StubAdapter {
	return &StubAdapter{name: name}
}

func (s *StubAdapter) Name() string {
	return s.name
}

func (s *StubAdapter) ParseInbound(ctx context.Context, raw []byte) (*Command, error) {
	return nil, ErrNotImplemented
}

func (s *StubAdapter) SendOutbound(ctx context.Context, userID model.UUID, event interface{}) error {
	return ErrNotImplemented
}

func (s *StubAdapter) Authenticate(ctx context.Context, raw []byte) (*model.User, error) {
	return nil, ErrNotImplemented
}

func (s *StubAdapter) IsEnabled() bool {
	return false
}

// ErrNotImplemented is returned by stub adapters.
var ErrNotImplemented = &channelError{"channel not implemented"}

type channelError struct {
	msg string
}

func (e *channelError) Error() string {
	return e.msg
}
