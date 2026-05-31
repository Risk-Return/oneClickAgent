package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oneClickAgent/gateway/internal/model"
)

func (deps *Dependencies) handleListVisibleSkills() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		user, err := deps.Users.GetByID(r.Context(), userID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		skills, err := deps.Vault.ListVisibleSkills(r.Context(), userID, user.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list skills")
			return
		}

		writeJSON(w, http.StatusOK, skills)
	}
}

func (deps *Dependencies) handleGetSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		user, err := deps.Users.GetByID(r.Context(), userID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		visible, err := deps.Vault.IsVisibleToUser(r.Context(), skillID, userID, user.OrgID)
		if err != nil || !visible {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "skill not found")
			return
		}

		skillWithVersion, err := deps.Vault.GetSkillWithLatestVersion(r.Context(), skillID)
		if err != nil || skillWithVersion == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "skill not found")
			return
		}

		writeJSON(w, http.StatusOK, skillWithVersion)
	}
}

func (deps *Dependencies) handleCreateSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.CreateSkillRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "name is required")
			return
		}
		if req.Visibility != model.VisibilityPublic && req.Visibility != model.VisibilityRestricted {
			req.Visibility = model.VisibilityRestricted
		}

		skill, err := deps.Vault.CreateSkill(r.Context(), req.Name, req.Description, req.Visibility)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create skill")
			return
		}

		writeJSON(w, http.StatusCreated, skill)
	}
}

func (deps *Dependencies) handleUpdateSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		skill, err := deps.Vault.GetSkill(r.Context(), skillID)
		if err != nil || skill == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "skill not found")
			return
		}

		var req model.CreateSkillRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.Name != "" {
			skill.Name = req.Name
		}
		if req.Description != "" {
			skill.Description = req.Description
		}
		if req.Visibility != "" {
			skill.Visibility = req.Visibility
		}

		if err := deps.Vault.UpdateSkill(r.Context(), skill); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update skill")
			return
		}

		writeJSON(w, http.StatusOK, skill)
	}
}

func (deps *Dependencies) handleDeleteSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		if err := deps.Vault.DeleteSkill(r.Context(), skillID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete skill")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "skill deleted"})
	}
}

func (deps *Dependencies) handlePublishSkillVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		if err := r.ParseMultipartForm(deps.Config.MaxUploadBytes); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "file too large")
			return
		}

		version := r.FormValue("version")
		manifest := r.FormValue("manifest")

		if version == "" || manifest == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "version and manifest are required")
			return
		}

		artifact, _, err := r.FormFile("artifact")
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "artifact file is required")
			return
		}
		defer artifact.Close()

		ver, err := deps.Vault.PublishVersion(r.Context(), skillID, version, manifest, artifact)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to publish version")
			return
		}

		writeJSON(w, http.StatusCreated, ver)
	}
}

func (deps *Dependencies) handleInstallSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		var req model.FleetSkillRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		ver, err := deps.Vault.GetVersionByTag(r.Context(), skillID, req.Version)
		if err != nil || ver == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "version not found")
			return
		}

		// Dispatch to all online devices
		_ = deps.Dispatch.DispatchToAllDevices(r.Context(), skillID, ver.ID)

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "skill installation initiated"})
	}
}

func (deps *Dependencies) handleDisableSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "not yet implemented"})
	}
}

func (deps *Dependencies) handleUpdateSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "not yet implemented"})
	}
}

func (deps *Dependencies) handleDeleteSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "not yet implemented"})
	}
}

func (deps *Dependencies) handleUpdateSkillVisibility() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		var req model.SkillVisibilityUpdate
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if err := deps.Vault.UpdateVisibility(r.Context(), skillID, req.Visibility); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update visibility")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "visibility updated"})
	}
}

func (deps *Dependencies) handleCreateSkillGrant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		var req model.SkillGrantRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if err := deps.Vault.GrantVisibility(r.Context(), skillID, req.PrincipalType, req.PrincipalID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create grant")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{"message": "grant created"})
	}
}

func (deps *Dependencies) handleDeleteSkillGrant() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		var req model.SkillGrantRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if err := deps.Vault.RevokeVisibility(r.Context(), skillID, req.PrincipalType, req.PrincipalID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete grant")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "grant removed"})
	}
}
