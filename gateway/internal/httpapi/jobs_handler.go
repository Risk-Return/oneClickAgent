package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oneClickAgent/gateway/internal/credvault"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
	"github.com/oneClickAgent/gateway/internal/pubsub"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
)

func (deps *Dependencies) handleSubmitJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)

		var req model.SubmitJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}
		defer r.Body.Close()

		if req.Command == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "command is required")
			return
		}

		// Validate one-skill-per-job constraint
		if req.SkillID != nil {
			// Verify skill exists and is visible
			user, err := deps.Users.GetByID(r.Context(), userID)
			if err != nil || user == nil {
				writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
				return
			}

			visible, err := deps.Vault.IsVisibleToUser(r.Context(), *req.SkillID, userID, user.OrgID)
			if err != nil || !visible {
				writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "skill not available")
				return
			}
		}

		job := &model.Job{
			UserID:  userID,
			Command: req.Command,
			SkillID: req.SkillID,
			Status:  model.JobPending,
		}

		if err := deps.Jobs.Create(r.Context(), job); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create job")
			return
		}

		// Link files to job
		for _, fileID := range req.FileIDs {
			_ = deps.Files.LinkToJob(r.Context(), fileID, job.ID, "input")
		}

		// Link credentials to job (before dispatch, for both immediate and queued paths)
		for _, credID := range req.CredentialIDs {
			_ = deps.Creds.LinkToJob(r.Context(), job.ID, credID)
		}

		// Try to allocate an agent
		agent, err := deps.Allocator.Allocate(r.Context(), job)
		if err != nil {
			if err.Error() == "queue full: max queued jobs per user reached" {
				writeError(w, http.StatusTooManyRequests, model.ErrCodeQueueFull, "max queued jobs per user reached")
			} else {
				writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "allocation failed")
			}
			return
		}

		if agent == nil {
			// Job queued - update status
			_ = deps.Jobs.UpdateStatus(r.Context(), job.ID, model.JobQueued)

			pos, _ := deps.Jobs.GetQueuePosition(r.Context(), job.ID)
			job.QueuePosition = &pos

			resp := model.JobResponse{Job: *job}
			if pos > 0 {
				estWait := pos * 30 // rough estimate
				resp.QueuePosition = &pos
				resp.EstimatedWaitSeconds = &estWait
			}

			writeJSON(w, http.StatusAccepted, resp)
			return
		}

		// Agent allocated - update job
		_ = deps.Jobs.SetAgent(r.Context(), job.ID, agent.ID, agent.DeviceID)
		_ = deps.PushFilesToDevice(r.Context(), job, agent.DeviceID)

		// Send JOB_DISPATCH frame to device
		dispatchPayload := model.JobDispatchPayload{
			JobID:         job.ID,
			UserID:        job.UserID,
			AgentID:       agent.ID,
			Command:       job.Command,
			SkillID:       job.SkillID,
			FileIDs:       req.FileIDs,
			CredentialIDs: req.CredentialIDs,
			SubmittedAt:   job.SubmittedAt.UnixMilli(),
		}
		if job.Params != nil {
			dispatchPayload.Params = *job.Params
		}
		dispatchFrame, frameErr := tunnel.NewFrame(model.FrameJobDispatch, dispatchPayload)
		var dispatchFailed bool
		if frameErr == nil {
			if sendErr := deps.Hub.SendFrame(agent.DeviceID, dispatchFrame); sendErr != nil {
				obs.Logger("http.jobs").Error("failed to send JOB_DISPATCH frame",
					"job_id", job.ID.String(),
					"agent_id", agent.ID.String(),
					"error", sendErr,
				)
				dispatchFailed = true
			}
		} else {
			dispatchFailed = true
		}
		if dispatchFailed {
			_ = deps.Allocator.Release(r.Context(), agent.ID)
			_ = deps.Jobs.UpdateStatus(r.Context(), job.ID, model.JobFailed)
			writeError(w, http.StatusServiceUnavailable, model.ErrCodeInternalError, "failed to dispatch job to device")
			return
		}

		// Push credentials
		_ = PushCredentialsForJob(r.Context(), job.ID, agent.ID, agent.DeviceID, deps.Creds, deps.CredVault, deps.Hub)

		// Publish event
		deps.Broker.PublishScoped(pubsub.JobTopic(job.ID), userID, model.WSEvent{
			Type:  model.WSEventJobStatus,
			Topic: pubsub.JobTopic(job.ID),
		})

		resp := model.JobResponse{
			Job:     *job,
			AgentID: &agent.ID,
		}

		writeJSON(w, http.StatusCreated, resp)
	}
}

func (deps *Dependencies) handleListJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		jobs, nextCursor, err := deps.Jobs.ListByUser(r.Context(), userID, cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list jobs")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.Job]{
			Items:       jobs,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
	}
}

func (deps *Dependencies) handleGetJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		jobID, err := model.ParseUUID(chi.URLParam(r, "jobID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid job_id")
			return
		}

		job, err := deps.Jobs.GetByID(r.Context(), jobID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if job == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "job not found")
			return
		}

		if job.UserID != userID {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		// Include queue info if queued
		if job.Status == model.JobQueued {
			pos, _ := deps.Jobs.GetQueuePosition(r.Context(), jobID)
			job.QueuePosition = &pos
		}

		writeJSON(w, http.StatusOK, job)
	}
}

func (deps *Dependencies) handleCancelJob() http.HandlerFunc {
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
		if job.Status.IsTerminal() {
			writeError(w, http.StatusConflict, model.ErrCodeConflict, "job already completed")
			return
		}

		if err := deps.Jobs.Cancel(r.Context(), jobID, userID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to cancel job")
			return
		}

		// Send JOB_CANCEL over tunnel if agent is allocated
		if job.AgentID != nil && job.DeviceID != nil {
			frame, _ := tunnel.NewFrame(model.FrameJobCancel, map[string]interface{}{
				"job_id":   jobID.String(),
				"agent_id": job.AgentID.String(),
				"reason":   "user requested",
			})
			_ = deps.Hub.SendFrame(*job.DeviceID, frame)
		}

		// Release agent if allocated
		if job.AgentID != nil {
			_ = deps.Allocator.Release(r.Context(), *job.AgentID)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "job cancelled"})
	}
}

func (deps *Dependencies) handleGetJobResult() http.HandlerFunc {
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

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"job_id": job.ID,
			"status": job.Status,
			"result": job.Result,
		})
	}
}

// PushFilesToDevice pushes files associated with a job to the device.
func (deps *Dependencies) PushFilesToDevice(ctx context.Context, job *model.Job, deviceID model.UUID) error {
	if deps.Relay == nil {
		return nil
	}
	return deps.Relay.PushJobInputs(ctx, job, deviceID)
}

// PushCredentialsForJob decrypts and pushes all linked credentials for a job over the tunnel.
func PushCredentialsForJob(ctx interface{}, jobID, agentID, deviceID model.UUID, credStore *store.CredentialStore, credVault *credvault.Vault, hub *tunnel.Hub) error {
	if !credVault.IsConfigured() {
		return nil
	}

	creds, err := credStore.ListByJob(ctx.(context.Context), jobID)
	if err != nil {
		return err
	}

	for _, cred := range creds {
		plaintext, err := credVault.Decrypt(cred.StorageStateEnc, cred.Nonce, cred.AuthTag, cred.SHA256)
		if err != nil {
			continue
		}
		frame, _ := tunnel.NewFrame(model.FrameCredPush, model.CredPushPayload{
			JobID:        jobID,
			CredentialID: cred.ID,
			AgentID:      agentID,
			Origin:       cred.Origin,
			StorageState: base64.StdEncoding.EncodeToString(plaintext),
			SHA256:       cred.SHA256,
		})
		_ = hub.SendFrame(deviceID, frame)
		_ = credStore.Touch(context.Background(), cred.ID)
	}

	return nil
}

// ─── Job Output File Handlers ────────────────────────────────

type jobOutputFileEntry struct {
	FileID    model.UUID `json:"file_id"`
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	SHA256    string     `json:"sha256"`
	CreatedAt time.Time  `json:"created_at"`
}

type jobOutputListResponse struct {
	JobID model.UUID             `json:"job_id"`
	Files []jobOutputFileEntry   `json:"files"`
}

func (deps *Dependencies) handleListJobOutputs() http.HandlerFunc {
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

		files, err := deps.Files.ListByJobAndRole(r.Context(), jobID, "output")
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list output files")
			return
		}

		entries := make([]jobOutputFileEntry, 0, len(files))
		for _, f := range files {
			entries = append(entries, jobOutputFileEntry{
				FileID:    f.ID,
				Name:      f.Name,
				Size:      f.Size,
				SHA256:    f.SHA256,
				CreatedAt: f.CreatedAt,
			})
		}

		writeJSON(w, http.StatusOK, jobOutputListResponse{
			JobID: jobID,
			Files: entries,
		})
	}
}

func (deps *Dependencies) handleDownloadJobOutput() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		jobID, err := model.ParseUUID(chi.URLParam(r, "jobID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid job_id")
			return
		}
		fileID, err := model.ParseUUID(chi.URLParam(r, "fileID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid file_id")
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

		file, err := deps.Files.GetByID(r.Context(), fileID)
		if err != nil || file == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not found")
			return
		}

		// Verify output role by checking job_files
		outputFiles, err := deps.Files.ListByJobAndRole(r.Context(), jobID, "output")
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		found := false
		for _, f := range outputFiles {
			if f.ID == fileID {
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not found")
			return
		}

		storagePath := file.StorageURI
		if storagePath == "" {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not available")
			return
		}

		// Defence in depth: reject if path escapes base dir
		absPath, err := filepath.Abs(storagePath)
		if err != nil || !filepath.HasPrefix(absPath, filepath.Clean(deps.Config.FileStore)) {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not found")
			return
		}

		f, err := os.Open(storagePath)
		if err != nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not found")
			return
		}
		defer f.Close()

		if r.URL.Query().Get("inline") == "1" {
			w.Header().Set("Content-Type", inlineContentType(file.Name))
			w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, file.Name))
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.Name))
		}
		w.Header().Set("ETag", fmt.Sprintf(`"%s"`, file.SHA256))
		w.WriteHeader(http.StatusOK)
		io.Copy(w, f)
	}
}

// inlineContentType maps an output file name to a browser-renderable content
// type for inline preview. Falls back to text/plain for unknown extensions.
func inlineContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".txt", ".log":
		return "text/plain; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
