package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oneClickAgent/gateway/internal/model"
)

func (deps *Dependencies) handleUploadFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)

		if err := r.ParseMultipartForm(deps.Config.MaxUploadBytes); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "file too large or invalid multipart form")
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "file field is required")
			return
		}
		defer file.Close()

		mimeType := header.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}

		stagedFile, err := deps.Relay.StageFile(r.Context(), userID, header.Filename, file, mimeType)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to stage file")
			return
		}

		writeJSON(w, http.StatusCreated, stagedFile)
	}
}

func (deps *Dependencies) handleListFiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		files, nextCursor, err := deps.Files.ListByUser(r.Context(), userID, cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list files")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.File]{
			Data:       files,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
	}
}

func (deps *Dependencies) handleGetFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		fileID, err := model.ParseUUID(chi.URLParam(r, "fileID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid file_id")
			return
		}

		file, err := deps.Files.GetByID(r.Context(), fileID)
		if err != nil || file == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not found")
			return
		}
		if file.UserID != userID {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		writeJSON(w, http.StatusOK, file)
	}
}

func (deps *Dependencies) handleDeleteFile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		fileID, err := model.ParseUUID(chi.URLParam(r, "fileID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid file_id")
			return
		}

		file, err := deps.Files.GetByID(r.Context(), fileID)
		if err != nil || file == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "file not found")
			return
		}
		if file.UserID != userID {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		if err := deps.Files.MarkPurged(r.Context(), fileID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete file")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "file deleted"})
	}
}
