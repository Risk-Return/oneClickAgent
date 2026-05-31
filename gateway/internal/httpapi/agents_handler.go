package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oneClickAgent/gateway/internal/model"
)

func (deps *Dependencies) handleListMyAgents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)

		agents, err := deps.Agents.ListByUser(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list agents")
			return
		}

		writeJSON(w, http.StatusOK, agents)
	}
}

func (deps *Dependencies) handleGetAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := model.ParseUUID(chi.URLParam(r, "agentID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid agent_id")
			return
		}

		agent, err := deps.Agents.GetByID(r.Context(), agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if agent == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "agent not found")
			return
		}

		claims := getClaims(r)
		if err := checkAgentAccess(claims, agent); err != nil {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		writeJSON(w, http.StatusOK, agent)
	}
}

func (deps *Dependencies) handleEnableAgentSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := model.ParseUUID(chi.URLParam(r, "agentID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid agent_id")
			return
		}

		var req model.EnableSkillRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		agent, err := deps.Agents.GetByID(r.Context(), agentID)
		if err != nil || agent == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "agent not found")
			return
		}

		claims := getClaims(r)
		if err := checkAgentAccess(claims, agent); err != nil {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		userID := getUserID(r)
		user, err := deps.Users.GetByID(r.Context(), userID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		visible, err := deps.Vault.IsVisibleToUser(r.Context(), req.SkillID, userID, user.OrgID)
		if err != nil || !visible {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "skill not visible to you")
			return
		}

		installed, err := deps.Dispatch.IsSkillInstalledOnDevice(r.Context(), agent.DeviceID, req.SkillID)
		if err != nil || !installed {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "skill not installed on device")
			return
		}

		ver, err := deps.Vault.GetLatestVersion(r.Context(), req.SkillID)
		if err != nil || ver == nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		as := &model.AgentSkill{
			AgentID:        agentID,
			SkillID:        req.SkillID,
			Status:         model.AgentSkillEnabled,
		}
		if err := deps.Skills.SetAgentSkill(r.Context(), as); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to enable skill")
			return
		}

		_ = deps.Dispatch.SendSkillAction(r.Context(), agent.DeviceID, model.SkillScopeAgent, model.SkillActionEnable, req.SkillID, ver.Version, &agentID)

		writeJSON(w, http.StatusOK, map[string]string{"message": "skill enabled"})
	}
}

func (deps *Dependencies) handleDisableAgentSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := model.ParseUUID(chi.URLParam(r, "agentID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid agent_id")
			return
		}

		skillID, err := model.ParseUUID(chi.URLParam(r, "skillID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid skill_id")
			return
		}

		agent, err := deps.Agents.GetByID(r.Context(), agentID)
		if err != nil || agent == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "agent not found")
			return
		}

		claims := getClaims(r)
		if err := checkAgentAccess(claims, agent); err != nil {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
			return
		}

		if err := deps.Skills.DeleteAgentSkill(r.Context(), agentID, skillID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to disable skill")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "skill disabled"})
	}
}

func (deps *Dependencies) handleAdminListAgents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		agents, nextCursor, err := deps.Agents.ListAll(r.Context(), cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list agents")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.Agent]{
			Data:       agents,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
	}
}

func (deps *Dependencies) handleDrainAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := model.ParseUUID(chi.URLParam(r, "agentID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid agent_id")
			return
		}

		if err := deps.Allocator.DrainAgent(r.Context(), agentID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to drain agent")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "agent draining"})
	}
}

func (deps *Dependencies) handleForceReleaseAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := model.ParseUUID(chi.URLParam(r, "agentID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid agent_id")
			return
		}

		if err := deps.Allocator.ForceRelease(r.Context(), agentID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to release agent")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "agent released"})
	}
}

func checkAgentAccess(claims interface{}, agent *model.Agent) error {
	return nil
}
