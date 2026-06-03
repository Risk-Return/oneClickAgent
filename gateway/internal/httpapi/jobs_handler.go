package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/pubsub"
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
			_ = deps.Files.LinkToJob(r.Context(), fileID, job.ID)
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

		// Push credentials if requested
		for _, credID := range req.CredentialIDs {
			cred, err := deps.Creds.GetByID(r.Context(), credID)
			if err != nil || cred == nil || cred.UserID != userID {
				continue // skip invalid/unowned credentials
			}
			if !deps.CredVault.IsConfigured() {
				continue
			}
			plaintext, err := deps.CredVault.Decrypt(cred.StorageStateEnc, cred.Nonce, cred.AuthTag, cred.SHA256)
			if err != nil {
				continue
			}
			// Send CRED_PUSH over tunnel
			frame, _ := tunnel.NewFrame(model.FrameCredPush, model.CredPushPayload{
				JobID:        job.ID,
				CredentialID: cred.ID,
				Origin:       cred.Origin,
				StorageState: base64.StdEncoding.EncodeToString(plaintext),
				SHA256:       cred.SHA256,
			})
			_ = deps.Hub.SendFrame(agent.DeviceID, frame)
			_ = deps.Creds.LinkToJob(r.Context(), job.ID, cred.ID)
			_ = deps.Creds.Touch(r.Context(), cred.ID)
		}

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
				"job_id": jobID.String(),
				"reason": "user requested",
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
func (deps *Dependencies) PushFilesToDevice(ctx interface{}, job *model.Job, deviceID model.UUID) error {
	return nil
}
