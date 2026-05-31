package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewUUID(t *testing.T) {
	id := NewUUID()
	if id.String() == "" {
		t.Error("UUID should not be empty")
	}
	id2 := NewUUID()
	if id == id2 {
		t.Error("sequential UUIDs should be unique")
	}
}

func TestParseUUID(t *testing.T) {
	id := NewUUID()
	str := id.String()
	parsed, err := ParseUUID(str)
	if err != nil {
		t.Fatalf("parse UUID: %v", err)
	}
	if parsed != id {
		t.Error("parsed UUID should match original")
	}
	_, err = ParseUUID("invalid")
	if err == nil {
		t.Error("should fail on invalid UUID")
	}
}

func TestUserRole(t *testing.T) {
	if !RoleAdmin.IsValid() {
		t.Error("admin role should be valid")
	}
	if !RoleUser.IsValid() {
		t.Error("user role should be valid")
	}
	if UserRole("invalid").IsValid() {
		t.Error("invalid role should not be valid")
	}
}

func TestUserTierPriority(t *testing.T) {
	if TierEnterprise.TierPriority() > TierPro.TierPriority() {
		t.Error("enterprise should have higher priority (lower number) than pro")
	}
	if TierPro.TierPriority() > TierFree.TierPriority() {
		t.Error("pro should have higher priority than free")
	}
	if TierFree.TierPriority() <= TierPro.TierPriority() {
		t.Error("free should have lower priority (higher number)")
	}
}

func TestJobStatusIsTerminal(t *testing.T) {
	terminals := []JobStatus{JobSucceeded, JobFailed, JobCancelled}
	for _, s := range terminals {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	nonTerminals := []JobStatus{JobPending, JobQueued, JobDispatched, JobRunning}
	for _, s := range nonTerminals {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestJobStatusIsActive(t *testing.T) {
	for _, s := range []JobStatus{JobPending, JobQueued, JobDispatched, JobRunning} {
		if !s.IsActive() {
			t.Errorf("%s should be active", s)
		}
	}
	for _, s := range []JobStatus{JobSucceeded, JobFailed, JobCancelled} {
		if s.IsActive() {
			t.Errorf("%s should not be active", s)
		}
	}
}

func TestFrameJSON(t *testing.T) {
	f := Frame{
		Version: FrameVersion,
		Type:    FramePing,
		MsgID:   "test-123",
		TS:      time.Now().UnixMilli(),
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	var decoded Frame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if decoded.Type != FramePing {
		t.Errorf("expected PING, got %s", decoded.Type)
	}
	if decoded.MsgID != "test-123" {
		t.Errorf("expected msg_id test-123, got %s", decoded.MsgID)
	}
}

func TestAPIErrorResponse(t *testing.T) {
	resp := APIErrorResponse{
		Error: APIError{
			Code:    ErrCodeNotFound,
			Message: "not found",
		},
	}
	data, _ := json.Marshal(resp)
	var decoded APIErrorResponse
	json.Unmarshal(data, &decoded)
	if decoded.Error.Code != ErrCodeNotFound {
		t.Errorf("expected NOT_FOUND, got %s", decoded.Error.Code)
	}
}
