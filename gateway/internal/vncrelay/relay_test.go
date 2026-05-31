package vncrelay

import (
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

func TestCreateSession(t *testing.T) {
	r := NewRelay("node-1", 4<<20, 2)

	jobID := model.NewUUID()
	userID := model.NewUUID()
	deviceID := model.NewUUID()
	agentID := model.NewUUID()

	sess, token, err := r.CreateSession(jobID, userID, deviceID, agentID, 300*time.Second, 1800*time.Second)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess == nil {
		t.Fatal("session should not be nil")
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if sess.UserID != userID {
		t.Errorf("userID = %s, want %s", sess.UserID, userID)
	}
	if sess.Status.Load() != statusPending {
		t.Error("session should be pending")
	}

	// Get session
	fetched := r.GetSession(sess.ID)
	if fetched == nil {
		t.Fatal("session should be retrievable")
	}

	// Mark ready
	if err := r.MarkReady(sess.ID, "password123"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if sess.RFBPassword != "password123" {
		t.Errorf("rfb password = %s", sess.RFBPassword)
	}
}

func TestSessionTokenAuth(t *testing.T) {
	r := NewRelay("node-1", 4<<20, 2)
	sess, token, _ := r.CreateSession(model.NewUUID(), model.NewUUID(), model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)

	// Wrong token should fail
	err := r.BindDevice(sess.ID, "wrong-token", nil)
	if err == nil {
		t.Error("wrong token should be rejected")
	}

	// Correct token should succeed (we pass nil ws, so it sets device)
	err = r.BindDevice(sess.ID, token, nil)
	if err != nil {
		t.Errorf("correct token should be accepted: %v", err)
	}
}

func TestPerUserConcurrency(t *testing.T) {
	r := NewRelay("node-1", 4<<20, 1)
	userID := model.NewUUID()

	_, _, err := r.CreateSession(model.NewUUID(), userID, model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)
	if err != nil {
		t.Fatalf("first session: %v", err)
	}

	// Second session should fail (cap=1)
	_, _, err = r.CreateSession(model.NewUUID(), userID, model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)
	if err == nil {
		t.Error("second session should be rejected (cap=1)")
	}
}

func TestCloseSession(t *testing.T) {
	r := NewRelay("node-1", 4<<20, 2)
	sess, _, _ := r.CreateSession(model.NewUUID(), model.NewUUID(), model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)

	r.CloseSession(sess.ID, "test close")
	if sess.Status.Load() != statusClosed {
		t.Error("session should be closed")
	}

	// Double close should not panic
	r.CloseSession(sess.ID, "double close")
}

func TestActiveCount(t *testing.T) {
	r := NewRelay("node-1", 4<<20, 2)

	if r.ActiveCount() != 0 {
		t.Error("active count should start at 0")
	}

	sess, _, _ := r.CreateSession(model.NewUUID(), model.NewUUID(), model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)
	_ = sess
	r.CreateSession(model.NewUUID(), model.NewUUID(), model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)

	// No sockets bound, so no activation
	if r.ActiveCount() != 0 {
		t.Error("active count should be 0 without bound sockets")
	}
}

func TestCloseUserSessions(t *testing.T) {
	r := NewRelay("node-1", 4<<20, 2)
	userID := model.NewUUID()

	s1, _, _ := r.CreateSession(model.NewUUID(), userID, model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)
	r.CreateSession(model.NewUUID(), userID, model.NewUUID(), model.NewUUID(), 300*time.Second, 1800*time.Second)

	r.CloseUserSessions(userID)

	if s1.Status.Load() != statusClosed {
		t.Error("user sessions should be closed")
	}
}
