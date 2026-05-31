package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/iagent/gateway/internal/auth"
	"github.com/iagent/gateway/internal/model"
	"github.com/iagent/gateway/internal/store"
)

func (deps *Dependencies) handleRegister() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}
		defer r.Body.Close()

		if req.Email == "" || req.Password == "" || req.Name == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "email, password, and name are required")
			return
		}

		if err := auth.ValidatePassword(req.Password); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, err.Error())
			return
		}

		existing, err := deps.Users.GetByEmail(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if existing != nil {
			writeError(w, http.StatusConflict, model.ErrCodeConflict, "email already registered")
			return
		}

		hashedPassword, err := deps.Hasher.Hash(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		user := &model.User{
			Email:        req.Email,
			PasswordHash: hashedPassword,
			Name:         req.Name,
			Role:         model.RoleUser,
			Tier:         model.TierFree,
		}

		if err := deps.Users.Create(r.Context(), user); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to create user")
			return
		}

		accessToken, err := deps.JWT.IssueAccessToken(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to issue token")
			return
		}

		refreshTokenStr, refreshTokenHash, err := auth.IssueRefreshToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to issue refresh token")
			return
		}

		rt := &model.RefreshToken{
			UserID:    user.ID,
			TokenHash: refreshTokenHash,
			Family:    model.NewUUID().String(),
			ExpiresAt: time.Now().UTC().Add(deps.Config.RefreshTTL),
		}
		if err := deps.Tokens.Create(r.Context(), rt); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to store refresh token")
			return
		}

		writeJSON(w, http.StatusCreated, model.AuthResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshTokenStr,
			ExpiresIn:    int(deps.Config.AccessTTL.Seconds()),
			User:         *user,
		})
	}
}

func (deps *Dependencies) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}
		defer r.Body.Close()

		if req.Email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "email and password are required")
			return
		}

		user, err := deps.Users.GetByEmail(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if user == nil {
			writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "invalid email or password")
			return
		}

		match, err := deps.Hasher.Verify(req.Password, user.PasswordHash)
		if err != nil || !match {
			writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "invalid email or password")
			return
		}

		accessToken, err := deps.JWT.IssueAccessToken(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to issue token")
			return
		}

		refreshTokenStr, refreshTokenHash, err := auth.IssueRefreshToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to issue refresh token")
			return
		}

		rt := &model.RefreshToken{
			UserID:    user.ID,
			TokenHash: refreshTokenHash,
			Family:    model.NewUUID().String(),
			ExpiresAt: time.Now().UTC().Add(deps.Config.RefreshTTL),
		}
		if err := deps.Tokens.Create(r.Context(), rt); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to store refresh token")
			return
		}

		writeJSON(w, http.StatusOK, model.AuthResponse{
			AccessToken:  accessToken,
			RefreshToken: refreshTokenStr,
			ExpiresIn:    int(deps.Config.AccessTTL.Seconds()),
			User:         *user,
		})
	}
}

func (deps *Dependencies) handleRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.RefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "invalid request body")
			return
		}
		defer r.Body.Close()

		if req.RefreshToken == "" {
			writeError(w, http.StatusBadRequest, model.ErrCodeValidationFailed, "refresh_token is required")
			return
		}

		tokenHash := auth.HashToken(req.RefreshToken)
		rt, err := deps.Tokens.GetByHash(r.Context(), tokenHash)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		if rt == nil || rt.RevokedAt != nil || time.Now().UTC().After(rt.ExpiresAt) {
			if rt != nil {
				_ = deps.Tokens.RevokeFamily(r.Context(), rt.Family)
			}
			writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "invalid or expired refresh token")
			return
		}

		if err := deps.Tokens.Revoke(r.Context(), rt.ID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}

		user, err := deps.Users.GetByID(r.Context(), rt.UserID)
		if err != nil || user == nil {
			writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "user not found")
			return
		}

		accessToken, err := deps.JWT.IssueAccessToken(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to issue token")
			return
		}

		newRefreshStr, newRefreshHash, err := auth.IssueRefreshToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to issue refresh token")
			return
		}

		newRT := &model.RefreshToken{
			UserID:    user.ID,
			TokenHash: newRefreshHash,
			Family:    rt.Family,
			ExpiresAt: time.Now().UTC().Add(deps.Config.RefreshTTL),
		}
		if err := deps.Tokens.Create(r.Context(), newRT); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "failed to store refresh token")
			return
		}

		writeJSON(w, http.StatusOK, model.AuthResponse{
			AccessToken:  accessToken,
			RefreshToken: newRefreshStr,
			ExpiresIn:    int(deps.Config.AccessTTL.Seconds()),
			User:         *user,
		})
	}
}

func (deps *Dependencies) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		if err := deps.Tokens.RevokeAllForUser(r.Context(), userID); err != nil {
			writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
	}
}

func (deps *Dependencies) handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := getUserID(r)
		user, err := deps.Users.GetByID(r.Context(), userID)
		if err != nil || user == nil {
			writeError(w, http.StatusNotFound, model.ErrCodeNotFound, "user not found")
			return
		}
		writeJSON(w, http.StatusOK, user)
	}
}

func handleHealthz(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleReadyz(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"error":  err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
