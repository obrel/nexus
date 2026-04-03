package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/egress"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/logger"
)

// wsProtocol is the negotiated sub-protocol name.
// Clients must send: Sec-WebSocket-Protocol: nexus-v1, <jwt_token>
const wsProtocol = "nexus-v1"

// Default heartbeat timings, used when config values are zero.
const (
	defaultPingPeriod = 30 * time.Second
	defaultPongWait   = 90 * time.Second
	defaultWriteWait  = 10 * time.Second
)

// Claims mirrors the HTTP tier JWT structure (sub = userID, tid = appID).
type Claims struct {
	Sub string `json:"sub"`
	Tid string `json:"tid"`
	jwt.RegisteredClaims
}

var upgrader = websocket.Upgrader{
	// Origin check is intentionally permissive; enforce at the load-balancer level.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler handles WebSocket upgrade requests with JWT sub-protocol authentication.
// The WebSocket is receive-only for downstream events. The only upstream messages
// accepted are typing indicators (TYP_START/TYP_STOP); all other frames are drained.
type Handler struct {
	jwtSecret  string
	mgr        *egress.ConnManager
	nc         *nats.Conn // for publishing typing indicators
	ring       *hash.Ring // for resolving group → shard
	log        logger.Logger
	pingPeriod time.Duration // how often the server sends a PING
	pongWait   time.Duration // read deadline (should be > pingPeriod)
	writeWait  time.Duration // deadline for writing control frames
}

// NewHandler creates a Handler that validates JWTs and delegates to mgr.
// WS timings are read from wsCfg; zero values fall back to defaults.
func NewHandler(jwtSecret string, mgr *egress.ConnManager, nc *nats.Conn, ring *hash.Ring, wsCfg *config.WSConfig) *Handler {
	pp := time.Duration(wsCfg.PingPeriod) * time.Second
	if pp <= 0 {
		pp = defaultPingPeriod
	}
	pw := time.Duration(wsCfg.PongWait) * time.Second
	if pw <= 0 {
		pw = defaultPongWait
	}
	ww := time.Duration(wsCfg.WriteWait) * time.Second
	if ww <= 0 {
		ww = defaultWriteWait
	}
	return &Handler{
		jwtSecret:  jwtSecret,
		mgr:        mgr,
		nc:         nc,
		ring:       ring,
		log:        logger.For("delivery", "ws"),
		pingPeriod: pp,
		pongWait:   pw,
		writeWait:  ww,
	}
}

// ServeHTTP upgrades the connection after JWT sub-protocol authentication.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Sub-protocol format: ["nexus-v1", "<jwt_token>"]
	protocols := websocket.Subprotocols(r)
	tokenString, ok := extractToken(protocols)
	if !ok {
		http.Error(w, "missing or invalid sub-protocol", http.StatusUnauthorized)
		return
	}

	appID, userID, err := h.parseJWT(tokenString)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade; acknowledge only the base protocol (not the raw JWT string).
	responseHeader := http.Header{}
	responseHeader.Set("Sec-Websocket-Protocol", wsProtocol)

	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		h.log.Warnf("websocket upgrade failed for user %s/%s: %v", appID, userID, err)
		return // upgrader already wrote the error response
	}

	ctx := r.Context()
	ctx = domain.WithAppID(ctx, appID)
	ctx = domain.WithUserID(ctx, userID)

	if err := h.mgr.Connect(ctx, conn, appID, userID); err != nil {
		h.log.Errorf("connect failed for user %s/%s: %v", appID, userID, err)
		conn.Close()
		return
	}

	// pingLoop sends server-initiated PINGs so browsers stay alive.
	// Runs concurrently with readLoop; terminates when ctx is done or conn closes.
	go h.pingLoop(ctx, conn)

	// readLoop blocks until the client disconnects.
	h.readLoop(ctx, conn, appID, userID)
}

// readLoop drains incoming frames and handles heartbeat refresh.
// The only upstream messages accepted are typing indicators (TYP_START/TYP_STOP);
// all other data frames are silently drained.
func (h *Handler) readLoop(ctx context.Context, conn *websocket.Conn, appID, userID string) {
	defer h.mgr.Disconnect(ctx, appID, userID)

	conn.SetReadDeadline(time.Now().Add(h.pongWait)) //nolint:errcheck

	// PONG handler: browser responds to server PING — reset deadline + refresh TTL.
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait)) //nolint:errcheck
		h.mgr.RefreshPresence(ctx, appID, userID)
		return nil
	})

	// PING handler: native clients send PING — reset deadline + refresh TTL + reply PONG.
	conn.SetPingHandler(func(appData string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait)) //nolint:errcheck
		h.mgr.RefreshPresence(ctx, appID, userID)
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(h.writeWait))
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return // EOF, close frame, or read deadline exceeded
		}
		if len(data) == 0 {
			continue
		}
		// Only handle typing indicators; all other frames are silently drained.
		h.handleTyping(appID, userID, data)
	}
}

// typingFrame represents TYP_START/TYP_STOP upstream messages.
// Clients send either group_id (for group typing) or user_id (for private typing).
type typingFrame struct {
	Request string `json:"request"`
	Data    struct {
		GroupID string `json:"group_id"`
		UserID  string `json:"user_id"`
	} `json:"data"`
}

// handleTyping publishes typing indicator events to the appropriate NATS subject.
// For group typing: publishes to the group's shard subject.
// For private typing: publishes to the recipient's private subject.
func (h *Handler) handleTyping(appID, userID string, data []byte) {
	var frame typingFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return
	}

	// Determine recipient from mutually exclusive fields.
	var recipientType domain.RecipientType
	var recipientID string
	switch {
	case frame.Data.GroupID != "" && frame.Data.UserID != "":
		return // invalid: both set
	case frame.Data.GroupID != "":
		recipientType = domain.RecipientGroup
		recipientID = frame.Data.GroupID
	case frame.Data.UserID != "":
		recipientType = domain.RecipientUser
		recipientID = frame.Data.UserID
	default:
		return // neither set
	}

	switch frame.Request {
	case "TYP_START", "TYP_STOP":
	default:
		return
	}

	if h.nc == nil {
		return
	}

	eventData := map[string]any{
		"recipient_type": string(recipientType),
		"recipient_id":   recipientID,
		"user_id":        userID,
		"typing":         frame.Request == "TYP_START",
	}

	var subject string
	if recipientType == domain.RecipientUser {
		subject = fmt.Sprintf("nexus.%s.v1.user.%s", appID, recipientID)
	} else {
		shard := h.ring.GetShard(recipientID)
		subject = fmt.Sprintf("nexus.%s.v1.grp.%d.%s", appID, shard, recipientID)
	}

	event := map[string]any{
		"event": "TYP_INDICATOR",
		"data":  eventData,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}

	if err := h.nc.Publish(subject, payload); err != nil {
		h.log.Warnf("ws: publish typing %s/%s: %v", appID, userID, err)
	}
}

// pingLoop sends a WebSocket PING frame every pingPeriod.
// WriteControl is safe to call concurrently with the reader goroutine.
func (h *Handler) pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(h.pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(h.writeWait)); err != nil {
				return // connection closed
			}
		}
	}
}

// extractToken returns the JWT from the sub-protocol list.
// Expects: [wsProtocol, "<jwt>"] in any order.
func extractToken(protocols []string) (string, bool) {
	var foundProtocol bool
	var token string
	for _, p := range protocols {
		if p == wsProtocol {
			foundProtocol = true
		} else if !strings.ContainsRune(p, ' ') && p != "" {
			token = p
		}
	}
	if foundProtocol && token != "" {
		return token, true
	}
	return "", false
}

func (h *Handler) parseJWT(tokenString string) (appID, userID string, err error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(h.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return "", "", jwt.ErrSignatureInvalid
	}
	if claims.Sub == "" || claims.Tid == "" {
		return "", "", jwt.ErrInvalidKey
	}
	return claims.Tid, claims.Sub, nil
}
