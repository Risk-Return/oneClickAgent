package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
	"github.com/oneClickAgent/gateway/internal/store"
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

// loggerMiddleware logs each request and records API metrics.
func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(ww.Status())

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)

		obs.APIRequests.WithLabelValues(r.Method, r.URL.Path, status).Inc()
		obs.APILatency.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
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

// tenantScopeMiddleware checks ownership for resource-scoped routes.
// It extracts the resource ID from the URL pattern (first path param) and validates
// that the authenticated user owns the resource. For job routes, it checks job.user_id.
// For file routes, it checks file.user_id.
func tenantScopeMiddleware(jobStore store.JobStoreInterface, fileStore store.FileStoreInterface) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := getClaims(r)
			if claims == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Admins bypass tenant scope
			if claims.Role == model.RoleAdmin {
				next.ServeHTTP(w, r)
				return
			}

			userID := getUserID(r)
			path := r.URL.Path

			// Check job resources
			if strings.Contains(path, "/jobs/") {
				parts := extractUUIDFromPath(path, "jobs")
				if parts != "" {
					if id, err := model.ParseUUID(parts); err == nil && jobStore != nil {
						if job, err := jobStore.GetByID(r.Context(), id); err == nil && job != nil {
							if job.UserID != userID {
								writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
								return
							}
						}
					}
				}
			}

			// Check file resources
			if strings.Contains(path, "/files/") {
				parts := extractUUIDFromPath(path, "files")
				if parts != "" {
					if id, err := model.ParseUUID(parts); err == nil && fileStore != nil {
						if file, err := fileStore.GetByID(r.Context(), id); err == nil && file != nil {
							if file.UserID != userID {
								writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "access denied")
								return
							}
						}
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractUUIDFromPath extracts a UUID segment after a given prefix in a path.
// e.g., extractUUIDFromPath("/api/v1/jobs/abc-123/cancel", "jobs") returns "abc-123".
func extractUUIDFromPath(path, prefix string) string {
	idx := strings.Index(path, "/"+prefix+"/")
	if idx < 0 {
		return ""
	}
	rest := path[idx+len(prefix)+2:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx > 0 {
		return rest[:slashIdx]
	}
	return rest
}

// ─── Security Headers (§12) ─────────────────────────────────

// securityHeadersMiddleware sets security-related HTTP headers on every response.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		next.ServeHTTP(w, r)
	})
}

// ─── CSRF Protection (§9) ────────────────────────────────────

// csrfMiddleware validates Origin/Referer headers for state-changing requests.
func csrfMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, o := range allowedOrigins {
		allowed[strings.ToLower(o)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method := strings.ToUpper(r.Method)
			if method == "GET" || method == "HEAD" || method == "OPTIONS" || method == "TRACE" {
				next.ServeHTTP(w, r)
				return
			}

			if len(allowed) == 0 || allowed["*"] {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			referer := r.Header.Get("Referer")
			if origin == "" && referer == "" {
				next.ServeHTTP(w, r)
				return
			}

			if origin != "" && allowed[strings.ToLower(origin)] {
				next.ServeHTTP(w, r)
				return
			}

			if referer != "" {
				for originHost := range allowed {
					if strings.HasPrefix(strings.ToLower(referer), originHost) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			writeError(w, http.StatusForbidden, model.ErrCodeForbidden, "cross-origin request denied")
		})
	}
}

// ─── Auth Rate Limiting (§9) ──────────────────────────────────

// authRateLimitMiddleware applies per-IP rate limiting for auth endpoints.
func authRateLimitMiddleware(maxPerMin int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	tokens := make(map[string][]time.Time)
	cleanupTick := time.NewTicker(time.Minute)

	go func() {
		for range cleanupTick.C {
			mu.Lock()
			cutoff := time.Now().Add(-2 * time.Minute)
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
			ipKey := r.RemoteAddr
			now := time.Now()
			window := now.Add(-time.Minute)

			mu.Lock()
			times := tokens[ipKey]
			var recent []time.Time
			for _, t := range times {
				if t.After(window) {
					recent = append(recent, t)
				}
			}
			if len(recent) >= maxPerMin {
				mu.Unlock()
				writeError(w, http.StatusTooManyRequests, model.ErrCodeLimitExceeded, "too many auth attempts, try again later")
				return
			}
			recent = append(recent, now)
			tokens[ipKey] = recent
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
}

// ─── Per-User Job Submission Rate Limiting (§9) ───────────────

// jobRateLimitMiddleware applies per-user rate limiting on job submission.
func jobRateLimitMiddleware(maxPerMin int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	tokens := make(map[model.UUID][]time.Time)
	cleanupTick := time.NewTicker(time.Minute)

	go func() {
		for range cleanupTick.C {
			mu.Lock()
			cutoff := time.Now().Add(-2 * time.Minute)
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
			if maxPerMin <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			userID := getUserID(r)
			now := time.Now()
			window := now.Add(-time.Minute)

			mu.Lock()
			times := tokens[userID]
			var recent []time.Time
			for _, t := range times {
				if t.After(window) {
					recent = append(recent, t)
				}
			}
			if len(recent) >= maxPerMin {
				mu.Unlock()
				writeError(w, http.StatusTooManyRequests, model.ErrCodeLimitExceeded, "too many job submissions, please wait")
				return
			}
			recent = append(recent, now)
			tokens[userID] = recent
			mu.Unlock()

			next.ServeHTTP(w, r)
		})
	}
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

func writeErrorWithRequest(r *http.Request, w http.ResponseWriter, status int, code model.ErrorCode, message string) {
	reqID := middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(model.APIErrorResponse{
		Error: model.APIError{
			Code:      code,
			Message:   message,
			RequestID: reqID,
		},
	})
}

func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
