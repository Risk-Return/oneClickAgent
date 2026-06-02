package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/oneClickAgent/gateway/internal/auth"
	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/pubsub"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Configure properly in production
	},
	Subprotocols: []string{"iagent.web.v1"},
}

func (deps *Dependencies) handleWebSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticate via query param or upgrade header
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("Sec-WebSocket-Protocol")
			// Try to extract from Authorization-like query
		}

		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := deps.JWT.VerifyToken(token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		userID, err := parseUserIDFromClaims(claims)
		if err != nil {
			http.Error(w, "invalid token claims", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("ws upgrade error", "error", err)
			return
		}
		defer conn.Close()

		subscriberID := userID.String() + "-" + model.NewUUID().String()[:8]

		// Subscribe to relevant topics
		jobSub := deps.Broker.Subscribe(pubsub.JobTopic(userID), subscriberID, userID)

		defer func() {
			deps.Broker.UnsubscribeAll(subscriberID)
		}()

		// Write pump (send events to client)
		go func() {
			for {
				select {
				case event, ok := <-jobSub.Ch:
					if !ok {
						return
					}
					data, err := json.Marshal(event)
					if err != nil {
						continue
					}
					if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
						return
					}
				}
			}
		}()

		// Send initial connection ack
		ack := map[string]interface{}{
			"type":    "connected",
			"user_id": userID,
		}
		ackData, _ := json.Marshal(ack)
		_ = conn.WriteMessage(websocket.TextMessage, ackData)

		// Read pump (handle client messages)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var wsMsg map[string]interface{}
			if err := json.Unmarshal(msg, &wsMsg); err != nil {
				continue
			}

			msgType, _ := wsMsg["type"].(string)
			switch msgType {
			case "subscribe":
				topic, _ := wsMsg["topic"].(string)
				if topic != "" {
					deps.Broker.Subscribe(topic, subscriberID, userID)
				}
			case "unsubscribe":
				topic, _ := wsMsg["topic"].(string)
				if topic != "" {
					deps.Broker.Unsubscribe(topic, subscriberID)
				}
			case "ping":
				pong := map[string]interface{}{"type": "pong"}
				pongData, _ := json.Marshal(pong)
				_ = conn.WriteMessage(websocket.TextMessage, pongData)
			}
		}
	}
}

func parseUserIDFromClaims(claims *auth.Claims) (model.UUID, error) {
	return model.ParseUUID(claims.Subject)
}
