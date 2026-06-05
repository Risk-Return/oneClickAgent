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

func (deps *Dependencies) handleAdminListSkills() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		skills, nextCursor, err := deps.Vault.ListSkills(r.Context(), cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list skills")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.Skill]{
			Items:      skills,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
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

		skill, err := deps.Vault.CreateSkill(r.Context(), req.Key, req.Name, req.Description, req.Visibility)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create skill")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.create", "skill", &skill.ID, nil)
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
		if req.Key != "" {
			skill.Key = req.Key
		}
		if req.Visibility != "" {
			skill.Visibility = req.Visibility
		}
		if req.Status != "" {
			skill.Status = model.SkillCatalogStatus(req.Status)
		}

		if err := deps.Vault.UpdateSkill(r.Context(), skill); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update skill")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.update", "skill", &skillID, nil)
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

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.delete", "skill", &skillID, nil)
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

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.publish_version", "skill", &skillID, nil)
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

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.fleet_install", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "skill installation initiated"})
	}
}

func (deps *Dependencies) handleDisableSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}
		// Send SKILL_ACTION to all devices
		devices, _ := deps.Devices.ListAll(r.Context())
		for _, d := range devices {
			_ = deps.Dispatch.SendSkillAction(r.Context(), d.ID, model.SkillScopeDevice, model.SkillActionDisable, skillID, "", nil)
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.fleet_disable", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "skill disable initiated"})
	}
}

func (deps *Dependencies) handleUpdateSkillFleet() http.HandlerFunc {
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
		_ = deps.Dispatch.DispatchToAllDevices(r.Context(), skillID, ver.ID)

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.fleet_update", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "skill update initiated"})
	}
}

func (deps *Dependencies) handleDeleteSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}
		devices, _ := deps.Devices.ListAll(r.Context())
		for _, d := range devices {
			_ = deps.Dispatch.SendSkillAction(r.Context(), d.ID, model.SkillScopeDevice, model.SkillActionDelete, skillID, "", nil)
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.fleet_delete", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "skill deletion initiated"})
	}
}

func (deps *Dependencies) handleRetrySkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		var req model.SkillRetryRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if err := deps.Dispatch.RetryFailedAgents(r.Context(), req.DeviceID, skillID, req.AgentIDs, nil); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to retry skill install")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.retry", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "retry initiated"})
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

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.update_visibility", "skill", &skillID, nil)
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

		if err := deps.Vault.GrantVisibility(r.Context(), skillID, req.PrincipalType, req.PrincipalID, getUserID(r)); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create grant")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.create_grant", "skill", &skillID, nil)
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

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.delete_grant", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "grant removed"})
	}
}

func (deps *Dependencies) handleDeleteSkillGrantPath() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		principalType := model.PrincipalType(chi.URLParam(r, "principal_type"))
		if principalType != model.PrincipalUser && principalType != model.PrincipalOrg {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "principal_type must be user or org")
			return
		}

		principalID, err := model.ParseUUID(chi.URLParam(r, "principal_id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid principal_id")
			return
		}

		if err := deps.Vault.RevokeVisibility(r.Context(), skillID, principalType, principalID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete grant")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.delete_grant", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "grant removed"})
	}
}

func (deps *Dependencies) handleListSkillGrants() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		grants, err := deps.Skills.ListGrants(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list grants")
			return
		}

		writeJSON(w, http.StatusOK, grants)
	}
}

func (deps *Dependencies) handleAdminGetSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		skillWithVersion, err := deps.Vault.GetSkillWithLatestVersion(r.Context(), skillID)
		if err != nil || skillWithVersion == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "skill not found")
			return
		}

		grants, _ := deps.Skills.ListGrants(r.Context(), skillID)

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"skill":           skillWithVersion,
			"grants":          grants,
		})
	}
}

func (deps *Dependencies) handleGetSkillRollout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		deviceSkills, err := deps.Skills.GetDeviceSkillsForSkill(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to get rollout status")
			return
		}

		agentSkills, err := deps.Skills.GetAgentSkillsForSkill(r.Context(), skillID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to get agent status")
			return
		}

		agentsByDevice := make(map[model.UUID][]model.SkillRolloutAgentEntry)
		for _, a := range agentSkills {
			agentsByDevice[a.DeviceID] = append(agentsByDevice[a.DeviceID], a)
		}

		var entries []model.SkillRolloutEntry
		for _, ds := range deviceSkills {
			deviceName := ""
			if device, _ := deps.Devices.GetByID(r.Context(), ds.DeviceID); device != nil {
				deviceName = device.Name
			}
			entries = append(entries, model.SkillRolloutEntry{
				DeviceID:   ds.DeviceID,
				DeviceName: deviceName,
				Version:    ds.Version,
				Status:     ds.Status,
				Error:      ds.ErrorMessage,
				UpdatedAt:  ds.UpdatedAt,
				Agents:     agentsByDevice[ds.DeviceID],
			})
		}

		writeJSON(w, http.StatusOK, entries)
	}
}

func (deps *Dependencies) handleEnableSkillFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		devices, _ := deps.Devices.ListAll(r.Context())
		for _, d := range devices {
			_ = deps.Dispatch.SendSkillAction(r.Context(), d.ID, model.SkillScopeDevice, model.SkillActionEnable, skillID, "", nil)
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "skill.fleet_enable", "skill", &skillID, nil)
		}

		writeJSON(w, http.StatusAccepted, map[string]string{"message": "skill enable initiated"})
	}
}
