package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/iagent/gateway/internal/model"
)

func (deps *Dependencies) handleDeviceEnroll() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.EnrollmentRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.EnrollmentCode == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "enrollment_code is required")
			return
		}

		device, err := deps.Devices.GetByEnrollmentCode(r.Context(), req.EnrollmentCode)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if device == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "invalid enrollment code")
			return
		}

		deviceToken := model.NewUUID().String() + "-" + model.NewUUID().String()
		tokenHash := hashTokenForStorage(deviceToken)
		if err := deps.Devices.UpdateToken(r.Context(), device.ID, tokenHash); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update device token")
			return
		}

		writeJSON(w, http.StatusOK, model.EnrollmentResponse{
			DeviceID:    device.ID,
			DeviceToken: deviceToken,
		})
	}
}

func (deps *Dependencies) handleListDevices() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		devices, nextCursor, err := deps.Devices.List(r.Context(), cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list devices")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.Device]{
			Data:       devices,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
	}
}

func (deps *Dependencies) handleGetDevice() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceID, err := model.ParseUUID(chi.URLParam(r, "deviceID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid device_id")
			return
		}

		device, err := deps.Devices.GetByID(r.Context(), deviceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if device == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "device not found")
			return
		}

		writeJSON(w, http.StatusOK, device)
	}
}

func (deps *Dependencies) handleDeleteDevice() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceID, err := model.ParseUUID(chi.URLParam(r, "deviceID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid device_id")
			return
		}

		if err := deps.Devices.Delete(r.Context(), deviceID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete device")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "device deleted"})
	}
}

func (deps *Dependencies) handleSetPoolSize() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceID, err := model.ParseUUID(chi.URLParam(r, "deviceID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid device_id")
			return
		}

		var req model.SetPoolSizeRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.Size < 1 {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "pool size must be at least 1")
			return
		}

		if err := deps.Devices.UpdatePoolSize(r.Context(), deviceID, req.Size); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update pool size")
			return
		}

		_ = deps.Allocator.EnsurePoolSize(r.Context(), deviceID, req.Size)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"device_id": deviceID,
			"pool_size": req.Size,
		})
	}
}

func (deps *Dependencies) handleRotateDeviceToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceID, err := model.ParseUUID(chi.URLParam(r, "deviceID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid device_id")
			return
		}

		deviceToken := model.NewUUID().String() + "-" + model.NewUUID().String()
		tokenHash := hashTokenForStorage(deviceToken)
		if err := deps.Devices.UpdateToken(r.Context(), deviceID, tokenHash); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to rotate token")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"device_id":    deviceID.String(),
			"device_token": deviceToken,
		})
	}
}

func hashTokenForStorage(token string) string {
	return model.NewUUID().String() // Placeholder - actual hashing would use auth.HashToken
}

func parseCursor(r *http.Request) *model.UUID {
	cursorStr := r.URL.Query().Get("cursor")
	if cursorStr == "" {
		return nil
	}
	id, err := model.ParseUUID(cursorStr)
	if err != nil {
		return nil
	}
	return &id
}

func parseLimit(r *http.Request, defaultLimit int) int {
	q := r.URL.Query().Get("limit")
	if q == "" {
		return defaultLimit
	}
	var limit int
	for _, c := range q {
		if c < '0' || c > '9' {
			return defaultLimit
		}
		limit = limit*10 + int(c-'0')
	}
	if limit == 0 || limit > 100 {
		return defaultLimit
	}
	return limit
}
