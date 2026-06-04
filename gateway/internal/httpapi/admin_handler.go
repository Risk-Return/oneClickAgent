package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oneClickAgent/gateway/internal/model"
)

func (deps *Dependencies) handleCreateOrg() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.CreateOrgRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "name is required")
			return
		}

		org := &model.Organization{
			Name:      req.Name,
			CreatedBy: getUserID(r),
		}
		if req.Description != "" {
			org.Description = req.Description
		}
		if err := deps.Orgs.Create(r.Context(), org); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create organization")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "org.create", "organization", &org.ID, nil)
		}

		writeJSON(w, http.StatusCreated, org)
	}
}

func (deps *Dependencies) handleListOrgs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		orgs, nextCursor, err := deps.Orgs.List(r.Context(), cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list organizations")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.Organization]{
			Items:       orgs,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
	}
}

func (deps *Dependencies) handleGetOrg() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := model.ParseUUID(chi.URLParam(r, "orgID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid org_id")
			return
		}

		org, err := deps.Orgs.GetByID(r.Context(), orgID)
		if err != nil || org == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "organization not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, org)
	}
}

func (deps *Dependencies) handleUpdateOrg() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := model.ParseUUID(chi.URLParam(r, "orgID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid org_id")
			return
		}

		org, err := deps.Orgs.GetByID(r.Context(), orgID)
		if err != nil || org == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "organization not found")
			return
		}

		var req model.CreateOrgRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.Name != "" {
			org.Name = req.Name
		}

		if err := deps.Orgs.Update(r.Context(), org); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update organization")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "org.update", "organization", &orgID, nil)
		}

		writeJSON(w, http.StatusOK, org)
	}
}

func (deps *Dependencies) handleDeleteOrg() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := model.ParseUUID(chi.URLParam(r, "orgID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid org_id")
			return
		}

		if err := deps.Orgs.Delete(r.Context(), orgID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to delete organization")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "org.delete", "organization", &orgID, nil)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "organization deleted"})
	}
}

func (deps *Dependencies) handleAddOrgMember() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := model.ParseUUID(chi.URLParam(r, "orgID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid org_id")
			return
		}

		var req model.UpdateOrgMemberRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if err := deps.Orgs.AddMember(r.Context(), orgID, req.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to add member")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "org.add_member", "organization", &orgID, nil)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "member added"})
	}
}

func (deps *Dependencies) handleRemoveOrgMember() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := model.ParseUUID(chi.URLParam(r, "orgID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid org_id")
			return
		}

		userID, err := model.ParseUUID(chi.URLParam(r, "userID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid user_id")
			return
		}

		if err := deps.Orgs.RemoveMember(r.Context(), userID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to remove member")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "org.remove_member", "organization", &orgID, nil)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "member removed"})
	}
}

func (deps *Dependencies) handleListOrgMembers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := model.ParseUUID(chi.URLParam(r, "orgID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid org_id")
			return
		}

		members, err := deps.Orgs.ListMembers(r.Context(), orgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list members")
			return
		}

		writeJSON(w, http.StatusOK, members)
	}
}

func (deps *Dependencies) handleListUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cursor := parseCursor(r)
		limit := parseLimit(r, 50)

		users, nextCursor, err := deps.Users.List(r.Context(), cursor, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to list users")
			return
		}

		writeJSON(w, http.StatusOK, model.PaginatedResponse[model.User]{
			Items:      users,
			NextCursor: nextCursor,
			HasMore:    nextCursor != nil,
		})
	}
}

func (deps *Dependencies) handleUpdateUserTier() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := model.ParseUUID(chi.URLParam(r, "userID"))
		if err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid user_id")
			return
		}

		var req model.UpdateUserTierRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}

		if req.Tier != model.TierFree && req.Tier != model.TierPro && req.Tier != model.TierEnterprise {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid tier")
			return
		}

		if err := deps.Users.UpdateTier(r.Context(), userID, req.Tier); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to update tier")
			return
		}

		if deps.Audit != nil {
			_ = deps.Audit.Log(r.Context(), getUserID(r), "user.update_tier", "user", &userID, nil)
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "tier updated"})
	}
}
