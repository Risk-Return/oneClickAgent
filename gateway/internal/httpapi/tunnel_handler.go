// Tunnel WebSocket endpoint: accepts device reverse-tunnel connections,
// validates device_token auth, upgrades to WS, and hands off to the tunnel Hub.
package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/obs"
	"github.com/oneClickAgent/gateway/internal/store"
	"github.com/oneClickAgent/gateway/internal/tunnel"
)

var tunnelUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
	Subprotocols:    []string{model.SubprotocolTunnel},
}

func (deps *Dependencies) handleTunnel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := obs.Logger("http.tunnel")

		// Extract device_token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "authorization required", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			http.Error(w, "invalid authorization format", http.StatusUnauthorized)
			return
		}

		// Hash token and look up device
		tokenHash := auth.HashToken(token)
		device, err := deps.Devices.GetByTokenHash(r.Context(), tokenHash)
		if err != nil {
			if err == store.ErrNotFound {
				http.Error(w, "invalid device token", http.StatusUnauthorized)
				return
			}
			logger.Error("device lookup failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Negotiate subprotocol
		subproto := r.Header.Get("Sec-WebSocket-Protocol")
		if subproto != "" && subproto != model.SubprotocolTunnel {
			logger.Warn("unsupported subprotocol requested", "subprotocol", subproto)
			http.Error(w, "unsupported subprotocol", http.StatusBadRequest)
			return
		}

		conn, err := tunnelUpgrader.Upgrade(w, r, http.Header{
			"Sec-WebSocket-Protocol": {model.SubprotocolTunnel},
		})
		if err != nil {
			logger.Error("tunnel ws upgrade error", "error", err)
			return
		}

		deviceConn := tunnel.NewDeviceConn(device.ID, conn)
		deviceConn.SetTokenHash(tokenHash)
		deviceConn.SetTokenVerifier(func(ctx context.Context, deviceID model.UUID, storedHash string) bool {
			d, err := deps.Devices.GetByID(ctx, deviceID)
			if err != nil || d == nil {
				return false
			}
			return d.TokenHash == storedHash
		})
		deps.Hub.Register(deviceConn)

		go deviceConn.StartReadPump(r.Context())
		go deviceConn.StartWritePump(r.Context())
		go deviceConn.StartTokenWatcher(r.Context())
	}
}