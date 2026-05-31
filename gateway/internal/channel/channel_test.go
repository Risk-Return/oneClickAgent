package channel

import (
	"context"
	"testing"

	"github.com/oneClickAgent/gateway/internal/model"
)

func TestStubAdapter(t *testing.T) {
	adapter := NewStubAdapter("feishu")
	if adapter.Name() != "feishu" {
		t.Errorf("expected feishu, got %s", adapter.Name())
	}
	if adapter.IsEnabled() {
		t.Error("stub adapter should not be enabled")
	}

	_, err := adapter.ParseInbound(context.Background(), nil)
	if err != ErrNotImplemented {
		t.Error("should return ErrNotImplemented")
	}

	err = adapter.SendOutbound(context.Background(), model.NewUUID(), nil)
	if err != ErrNotImplemented {
		t.Error("should return ErrNotImplemented")
	}

	_, err = adapter.Authenticate(context.Background(), nil)
	if err != ErrNotImplemented {
		t.Error("should return ErrNotImplemented")
	}
}
