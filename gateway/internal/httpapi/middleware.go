package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/model"
)

type contextKey string

const (
	ctxKeyClaims contextKey = "claims"
	ctxKeyUserID contextKey = "user_id"
	ctxKeyRole   contextKey = "role"
)

// requestIDMiddleware ensures every request has a unique ID.
func requestIDMiddleware(next http.Handler) http.Handler {
	return middleware.RequestID(next)
}

// recoverMiddleware recovers from panics and returns a 500.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
					"request_id", middleware.GetReqID(r.Context()),
				)
				writeError(w, http.StatusInternalServerError, model.ErrCodeInternalError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware applies a simple per-second rate limit.
func rateLimitMiddleware(perSec int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	tokens := make(map[string][]time.Time)
	cleanupTick := time.NewTicker(time.Minute)

	go func() {
		for range cleanupTick.C {
			mu.Lock()
			cutoff := time.Now().Add(-time.Minute)
			for k, times := range tokens {
				var valid []time.Time
				for _, t := range times {
					if t.After(cutoff) {
						valid = append(valid, t)
					}
				}
				if len(valid) == 0 {
					delete(tokens, k)
				} else {
					tokens[k] = valid
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.RemoteAddr
			now := time.Now()
			window := now.Add(-time.Second)

			mu.Lock()
			times := tokens[key]
			var recent []time.Time
			for _, t := range times {
				if t.After(window) {
					recent = append(recent, t)
				}
			}
			if len(recent) >= perSec {
				mu.Unlock()
				writeError(w, http.StatusTooManyRequests, model.ErrCodeLimitExceeded, "rate limit exceeded")
				return
			}
			recent = append(recent, now)
			tokens[key] = recent
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

// loggerMiddleware logs each request.
func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// authMiddleware validates the JWT and injects claims into the context.
func authMiddleware(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "invalid authorization format")
				return
			}

			claims, err := jwtManager.VerifyToken(parts[1])
			if err != nil {
				writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "invalid or expired token")
				return
			}

			userID, err := auth.ExtractUserID(claims)
			if err != nil {
				writeError(w, http.StatusUnauthorized, model.ErrCodeUnauthorized, "invalid token claims")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyClaims, claims)
			ctx = context.WithValue(ctx, ctxKeyUserID, userID)
			ctx = context.WithValue(ctx, ctxKeyRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// requireAdminMiddleware ensures the authenticated user is an admin.
func requireAdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := getClaims(r)
		if err := auth.RequireAdmin(claims); err != nil {
			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Helpers to extract info from context

func getClaims(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(ctxKeyClaims).(*auth.Claims)
	return claims
}

func getUserID(r *http.Request) model.UUID {
	id, _ := r.Context().Value(ctxKeyUserID).(model.UUID)
	return id
}

func getRole(r *http.Request) model.UserRole {
	role, _ := r.Context().Value(ctxKeyRole).(model.UserRole)
	return role
}

// Response helpers

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code model.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(model.APIErrorResponse{
		Error: model.APIError{
			Code:    code,
			Message: message,
		},
	})
}

func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
