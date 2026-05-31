package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/tunnel"
)

func (deps *Dependencies) handleOpenVNC() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		jobID, err := model.ParseUUID(chi.URLParam(r, "jobID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid job_id")
			return
		}

		job, err := deps.Jobs.GetByID(r.Context(), jobID)
		if err != nil || job == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "job not found")
			return
		}
		if job.UserID != userID {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}
		if job.Status != model.JobRunning {
			writeError(w, http.StatusConflict, model.ErrCodeConflict, "job is not running")
			return
		}
		if job.AgentID == nil || job.DeviceID == nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "job has no assigned agent")
			return
		}

		idleTTL := deps.Config.VNCIdleTTLSecs
		maxTTL := deps.Config.VNCMaxTTLSecs

		sess, token, err := deps.VNCRelay.CreateSession(jobID, userID, *job.DeviceID, *job.AgentID, msToDur(idleTTL), msToDur(maxTTL))
		if err != nil {
			writeError(w, http.StatusTooManyRequests, model.ErrCodeLimitExceeded, err.Error())
			return
		}

		// Send VNC_OPEN over tunnel
		frame, _ := tunnel.NewFrame(model.FrameVNCOpen, model.VNCOpenPayload{
			SessionID:    sess.ID,
			RelayURL:     "wss://" + r.Host + "/session/" + sess.ID.String(),
			SessionToken: token,
			TTLSecs:      maxTTL,
		})
		_ = deps.Hub.SendFrame(*job.DeviceID, frame)

		writeJSON(w, http.StatusCreated, model.VNCOpenResponse{
			SessionID:   sess.ID,
			WSUrl:       "/ws/vnc/" + sess.ID.String(),
			TTLSecs:     maxTTL,
		})
	}
}

func (deps *Dependencies) handleCloseVNC() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := model.ParseUUID(chi.URLParam(r, "sessionID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid session_id")
			return
		}
		deps.VNCRelay.CloseSession(sessionID, "user requested")
		writeJSON(w, http.StatusOK, map[string]string{"message": "session closed"})
	}
}

func (deps *Dependencies) handleSaveLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := model.ParseUUID(chi.URLParam(r, "sessionID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid session_id")
			return
		}

		sess := deps.VNCRelay.GetSession(sessionID)
		if sess == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "session not found")
			return
		}

		var req model.SaveLoginRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request")
			return
		}

		// Send CRED_CAPTURE to device
		frame, _ := tunnel.NewFrame(model.FrameCredCapture, model.CredCapturePayload{
			SessionID: sessionID,
			Origin:    "",
			Label:     req.Label,
		})
		_ = deps.Hub.SendFrame(sess.DeviceID, frame)

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "capture initiated"})
	}
}

func (deps *Dependencies) handleListCredentials() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		creds, err := deps.Creds.ListByUser(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list credentials")
			return
		}

		var resp []model.CredentialResponse
		for i := range creds {
			resp = append(resp, model.CredentialResponseFrom(&creds[i]))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func (deps *Dependencies) handleDeleteCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		credID, err := model.ParseUUID(chi.URLParam(r, "credentialID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid credential_id")
			return
		}

		cred, err := deps.Creds.GetByID(r.Context(), credID)
		if err != nil || cred == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "credential not found")
			return
		}
		if cred.UserID != userID {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		if err := deps.Creds.Delete(r.Context(), credID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete credential")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "credential deleted"})
	}
}

func msToDur(secs int) time.Duration { return time.Duration(secs) * time.Second }

// ─── VNC WebSocket Handlers ─────────────────────────────────

var vncUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
	Subprotocols:    []string{"iagent.web.v1"},
}

var vncDeviceUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
	Subprotocols:    []string{"iagent.session.v1"},
}

func (deps *Dependencies) handleVNCBrowserSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "token required", http.StatusUnauthorized)
			return
		}

		claims, err := deps.JWT.VerifyToken(token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		userID, err := model.ParseUUID(claims.Subject)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		sessionID, err := model.ParseUUID(chi.URLParam(r, "sessionID"))
		if err != nil {
			http.Error(w, "invalid session_id", http.StatusBadRequest)
			return
		}

		conn, err := vncUpgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("vnc browser ws upgrade", "error", err)
			return
		}

		if err := deps.VNCRelay.BindBrowser(sessionID, userID, conn); err != nil {
			conn.Close()
			slog.Error("vnc browser bind", "error", err)
		}
	}
}

func (deps *Dependencies) handleVNCDeviceSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionToken := r.Header.Get("X-Session-Token")
		if sessionToken == "" {
			sessionToken = r.URL.Query().Get("token")
		}
		if sessionToken == "" {
			http.Error(w, "token required", http.StatusUnauthorized)
			return
		}

		sessionID, err := model.ParseUUID(chi.URLParam(r, "sessionID"))
		if err != nil {
			http.Error(w, "invalid session_id", http.StatusBadRequest)
			return
		}

		conn, err := vncDeviceUpgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("vnc device ws upgrade", "error", err)
			return
		}

		if err := deps.VNCRelay.BindDevice(sessionID, sessionToken, conn); err != nil {
			conn.Close()
			slog.Error("vnc device bind", "error", err)
		}
	}
}
