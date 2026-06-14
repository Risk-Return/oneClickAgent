// Package vncrelay manages interactive VNC session pairing and binary byte
// relay between browser (noVNC) and device-side agent.
//
// The gateway acts as a transparent RFB proxy: it never parses the RFB
// protocol, just pumps bytes between sockets.
package vncrelay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
)

// Session represents an active VNC relay pairing.
type Session struct {
	ID         model.UUID
	JobID      model.UUID
	UserID     model.UUID
	DeviceID   model.UUID
	AgentID    model.UUID
	TokenHash  string
	RFBPassword string
	Status     atomic.Int32               // 0=pending, 1=ready, 2=active, 3=closed

	browser    *websocket.Conn
	device     *websocket.Conn

	mu         sync.Mutex
	lastActive atomic.Int64

	readyCh    chan struct{}
	readyErr   string

	idleTTL    time.Duration
	maxTTL     time.Duration
	createdAt  time.Time
	closedAt   time.Time
	bufferCap  int64
}

const (
	statusPending = 0
	statusReady   = 1
	statusActive  = 2
	statusClosed  = 3
)

// Relay manages VNC session pairing and byte relay.
type Relay struct {
	mu        sync.RWMutex
	sessions  map[model.UUID]*Session // sessionID → Session
	logger    *slog.Logger
	nodeID    string
	bufferCap int64
	maxPerUser int
}

// NewRelay creates a new VNC relay manager.
func NewRelay(nodeID string, bufferCap int64, maxPerUser int) *Relay {
	return &Relay{
		sessions:   make(map[model.UUID]*Session),
		logger:     obs.Logger("vncrelay"),
		nodeID:     nodeID,
		bufferCap:  bufferCap,
		maxPerUser: maxPerUser,
	}
}

// CreateSession initialises a pending VNC session.
// If a non-closed session already exists for this job, it is returned
// (idempotent — the frontend may retry VNC open on reconnect).
func (r *Relay) CreateSession(jobID, userID, deviceID, agentID model.UUID, idleTTL, maxTTL time.Duration) (*Session, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If there's already a live session for this job, return it with a fresh token.
	for _, s := range r.sessions {
		if s.JobID == jobID && s.Status.Load() != statusClosed {
			newToken := generateSessionToken()
			s.TokenHash = hashToken(newToken)
			r.logger.Info("reusing existing VNC session for job", "session_id", s.ID, "job_id", jobID)
			return s, newToken, nil
		}
	}

	sessionID := model.NewUUID()
	sessionToken := generateSessionToken()
	tokenHash := hashToken(sessionToken)

	sess := &Session{
		ID:         sessionID,
		JobID:      jobID,
		UserID:     userID,
		DeviceID:   deviceID,
		AgentID:    agentID,
		TokenHash:  tokenHash,
		idleTTL:    idleTTL,
		maxTTL:     maxTTL,
		bufferCap: r.bufferCap,
		createdAt:  time.Now(),
		readyCh:    make(chan struct{}),
	}
	sess.Status.Store(statusPending)

	r.sessions[sessionID] = sess
	r.logger.Info("VNC session created", "session_id", sessionID, "job_id", jobID)

	return sess, sessionToken, nil
}

// GetSession returns a session by ID.
func (r *Relay) GetSession(sessionID model.UUID) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[sessionID]
}

// MarkReady transitions a session from pending to ready.
func (r *Relay) MarkReady(sessionID model.UUID, rfbPassword string) error {
	sess := r.GetSession(sessionID)
	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	sess.mu.Lock()
	sess.RFBPassword = rfbPassword
	sess.mu.Unlock()
	sess.Status.Store(statusReady)
	r.logger.Info("MarkReady", "session_id", sessionID, "has_password", rfbPassword != "")

	// Fire ready channel for async waiters
	select {
	case <-sess.readyCh:
	default:
		close(sess.readyCh)
	}
	return nil
}

// MarkError marks a session as failed with an error message.
func (r *Relay) MarkError(sessionID model.UUID, errMsg string) {
	sess := r.GetSession(sessionID)
	if sess == nil {
		return
	}
	sess.readyErr = errMsg
	r.logger.Info("MarkError", "session_id", sessionID, "error", errMsg)
	select {
	case <-sess.readyCh:
	default:
		close(sess.readyCh)
	}
	r.CloseSession(sessionID, "device error: "+errMsg)
}

// WaitReady blocks until the session is ready or an error occurs, up to timeout.
func (r *Relay) WaitReady(sessionID model.UUID, timeout time.Duration) (password string, err error) {
	sess := r.GetSession(sessionID)
	if sess == nil {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	r.logger.Info("WaitReady waiting for session", "session_id", sessionID, "status", sess.Status.Load(), "readyCh", fmt.Sprintf("%p", sess.readyCh))

	select {
	case <-sess.readyCh:
		sess.mu.Lock()
		password = sess.RFBPassword
		errMsg := sess.readyErr
		sess.mu.Unlock()
		r.logger.Info("WaitReady unblocked", "session_id", sessionID, "has_password", password != "", "error", errMsg, "readyCh", fmt.Sprintf("%p", sess.readyCh))
		if errMsg != "" {
			return "", fmt.Errorf("VNC session error: %s", errMsg)
		}
		return password, nil
	case <-time.After(timeout):
		r.logger.Warn("WaitReady timed out", "session_id", sessionID, "timeout", timeout)
		return "", fmt.Errorf("VNC session timed out waiting for ready")
	}
}

// BindBrowser binds the noVNC (browser) WebSocket to a session.
func (r *Relay) BindBrowser(sessionID model.UUID, userID model.UUID, ws *websocket.Conn) error {
	sess := r.GetSession(sessionID)
	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if sess.UserID != userID {
		return fmt.Errorf("session belongs to different user")
	}

	sess.mu.Lock()
	if sess.browser != nil {
		sess.mu.Unlock()
		return fmt.Errorf("browser already connected")
	}
	sess.browser = ws
	sess.mu.Unlock()

	r.maybeActivate(sess)
	return nil
}

// BindDevice binds the device-side WebSocket to a session.
func (r *Relay) BindDevice(sessionID model.UUID, token string, ws *websocket.Conn) error {
	sess := r.GetSession(sessionID)
	if sess == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if hashToken(token) != sess.TokenHash {
		return fmt.Errorf("invalid session token")
	}

	sess.mu.Lock()
	if sess.device != nil {
		sess.mu.Unlock()
		return fmt.Errorf("device already connected")
	}
	sess.device = ws
	sess.mu.Unlock()

	r.maybeActivate(sess)
	return nil
}

func (r *Relay) maybeActivate(sess *Session) {
	sess.mu.Lock()
	browser := sess.browser
	device := sess.device
	sess.mu.Unlock()

	if browser != nil && device != nil {
		sess.Status.Store(statusActive)
		sess.touch()
		r.logger.Info("VNC session active", "session_id", sess.ID)
		go r.pump(sess, browser, device, "browser→device")
		go r.pump(sess, device, browser, "device→browser")
	}
}

// pump copies binary messages from src to dst with backpressure.
func (r *Relay) pump(sess *Session, src, dst *websocket.Conn, label string) {
	defer func() {
		r.CloseSession(sess.ID, label+" closed")
	}()

	msgBuf := make([]byte, sess.bufferCap)
	for {
		if sess.Status.Load() == statusClosed {
			return
		}

		msgType, reader, err := src.NextReader()
		if err != nil {
			return
		}

		if msgType != websocket.BinaryMessage {
			continue
		}

		writer, err := dst.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return
		}

		if _, err := io.CopyBuffer(writer, reader, msgBuf); err != nil {
			_ = writer.Close()
			return
		}

		if err := writer.Close(); err != nil {
			return
		}

		sess.touch()
	}
}

func (s *Session) touch() {
	s.lastActive.Store(time.Now().Unix())
}

// CloseSession closes a VNC session and both sockets.
func (r *Relay) CloseSession(sessionID model.UUID, reason string) {
	sess := r.GetSession(sessionID)
	if sess == nil {
		return
	}
	if !sess.Status.CompareAndSwap(statusPending, statusClosed) &&
		!sess.Status.CompareAndSwap(statusReady, statusClosed) &&
		!sess.Status.CompareAndSwap(statusActive, statusClosed) {
		return
	}

	sess.closedAt = time.Now()

	sess.mu.Lock()
	defer sess.mu.Unlock()

	if sess.browser != nil {
		sess.browser.Close()
	}
	if sess.device != nil {
		sess.device.Close()
	}

	r.logger.Info("VNC session closed", "session_id", sessionID, "reason", reason)
}

// CloseUserSessions closes all active sessions for a user.
func (r *Relay) CloseUserSessions(userID model.UUID) {
	r.mu.RLock()
	var toClose []model.UUID
	for id, s := range r.sessions {
		if s.UserID == userID && s.Status.Load() != statusClosed {
			toClose = append(toClose, id)
		}
	}
	r.mu.RUnlock()

	for _, id := range toClose {
		r.CloseSession(id, "user sessions closed")
	}
}

// ActiveCount returns the number of active sessions.
func (r *Relay) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, s := range r.sessions {
		if s.Status.Load() == statusActive {
			count++
		}
	}
	return count
}

// StartReaper starts a background goroutine that closes idle and expired sessions.
func (r *Relay) StartReaper(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reap()
		}
	}
}

func (r *Relay) reap() {
	now := time.Now()

	r.mu.RLock()
	var toClose []model.UUID
	for id, s := range r.sessions {
		status := s.Status.Load()
		if status == statusClosed {
			continue
		}

		// Max TTL check
		if now.Sub(s.createdAt) > s.maxTTL {
			toClose = append(toClose, id)
			continue
		}

		// Idle check (only for active sessions)
		if status == statusActive {
			last := time.Unix(s.lastActive.Load(), 0)
			if now.Sub(last) > s.idleTTL {
				toClose = append(toClose, id)
			}
		}

		// Pending/ready sessions also expire after max TTL
		if (status == statusPending || status == statusReady) && now.Sub(s.createdAt) > s.maxTTL {
			toClose = append(toClose, id)
		}
	}
	r.mu.RUnlock()

	for _, id := range toClose {
		r.CloseSession(id, "reaper: TTL expired")
	}
}

// ─── Helpers ────────────────────────────────────────────────

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
