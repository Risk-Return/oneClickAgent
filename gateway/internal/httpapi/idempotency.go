package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

type idempotencyEntry struct {
	status     int
	body       []byte
	expiresAt  time.Time
}

var (
	idempotencyMu   sync.Mutex
	idempotencyCache = make(map[string]*idempotencyEntry)
)

const idempotencyTTL = 24 * time.Hour

func init() {
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			idempotencyMu.Lock()
			now := time.Now()
			for k, v := range idempotencyCache {
				if now.After(v.expiresAt) {
					delete(idempotencyCache, k)
				}
			}
			idempotencyMu.Unlock()
		}
	}()
}

func idempotencyKey(r *http.Request) string {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return ""
	}
	h := sha256.Sum256([]byte(r.URL.Path + "|" + key))
	return hex.EncodeToString(h[:])
}

func idempotencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ikey := idempotencyKey(r)
		if ikey == "" {
			next.ServeHTTP(w, r)
			return
		}

		idempotencyMu.Lock()
		entry, exists := idempotencyCache[ikey]
		if exists && time.Now().Before(entry.expiresAt) {
			idempotencyMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(entry.status)
			_, _ = w.Write(entry.body)
			return
		}
		idempotencyMu.Unlock()

		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		if rec.statusCode >= 200 && rec.statusCode < 500 {
			idempotencyMu.Lock()
			idempotencyCache[ikey] = &idempotencyEntry{
				status:    rec.statusCode,
				body:      rec.body.Bytes(),
				expiresAt: time.Now().Add(idempotencyTTL),
			}
			idempotencyMu.Unlock()
		}
	})
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}
