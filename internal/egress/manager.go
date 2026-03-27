package egress

import (
	"context"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
)

// defaultPresenceTTL is the fallback Redis key expiry in seconds when not configured.
const defaultPresenceTTL = 60

// ControlHandler processes inter-tier control messages received via NATS.
type ControlHandler interface {
	HandleControl(appID, userID string, data []byte)
}

// DisconnectHook is called when a user disconnects.
type DisconnectHook func(appID, userID string)

// ConnManager tracks active WebSocket connections in local memory,
// maintains Redis presence for global routing, and broadcasts
// online/offline events to NATS on connect and disconnect.
//
// sync.Map is used because connection access is read-heavy (message delivery)
// with rare writes (connect/disconnect).
type ConnManager struct {
	conns    sync.Map // key: "appID:userID" → *websocket.Conn
	subs     sync.Map // key: "appID:userID" → *nats.Subscription
	ctrlSubs sync.Map // key: "appID:userID" → *nats.Subscription (control subject)
	writeMus sync.Map // key: "appID:userID" → *sync.Mutex
	presence domain.PresenceRepository
	nc       *nats.Conn // nil in tests; NATS ops are skipped when nil
	nodeID   string
	log      logger.Logger

	presenceTTL          int // seconds; 0 → defaultPresenceTTL
	subPendingMsgsLimit  int // NATS subscription pending messages limit
	subPendingBytesLimit int // NATS subscription pending bytes limit

	ctrlHandler    ControlHandler
	disconnectHooks []DisconnectHook
}

// NewConnManager creates a ConnManager for the given node.
// nc may be nil — presence events and private subscriptions are silently skipped.
func NewConnManager(presence domain.PresenceRepository, nc *nats.Conn, nodeID string) *ConnManager {
	return &ConnManager{
		presence: presence,
		nc:       nc,
		nodeID:   nodeID,
		log:      logger.For("egress", "conn_manager"),
	}
}

// SetPresenceTTL configures the Redis presence key expiry in seconds.
func (m *ConnManager) SetPresenceTTL(ttl int) {
	m.presenceTTL = ttl
}

// SetSubPendingLimits configures NATS subscription pending message and byte limits.
func (m *ConnManager) SetSubPendingLimits(msgs, bytes int) {
	m.subPendingMsgsLimit = msgs
	m.subPendingBytesLimit = bytes
}

// getPresenceTTL returns the configured TTL or the default.
func (m *ConnManager) getPresenceTTL() int {
	if m.presenceTTL > 0 {
		return m.presenceTTL
	}
	return defaultPresenceTTL
}

// SetControlHandler registers a handler for inter-tier control messages.
func (m *ConnManager) SetControlHandler(h ControlHandler) {
	m.ctrlHandler = h
}

// OnDisconnect registers a hook called when a user disconnects.
func (m *ConnManager) OnDisconnect(hook DisconnectHook) {
	m.disconnectHooks = append(m.disconnectHooks, hook)
}

// Connect registers conn in local memory, sets Redis presence, subscribes to
// the user's private NATS subject, and publishes an "online" event.
// If the user already has an active connection on this node it is closed first.
func (m *ConnManager) Connect(ctx context.Context, conn *websocket.Conn, appID, userID string) error {
	key := connKey(appID, userID)

	// Replace any existing connection for this user.
	if old, loaded := m.conns.Swap(key, conn); loaded {
		old.(*websocket.Conn).Close() //nolint:errcheck — best-effort close
		m.log.Infof("replaced existing connection for user %s/%s", appID, userID)
	}

	if err := m.presence.SetUserPresence(ctx, appID, userID, m.nodeID, m.getPresenceTTL()); err != nil {
		m.conns.Delete(key)
		return fmt.Errorf("conn manager: set presence: %w", err)
	}

	// Subscribe to private NATS subject: nexus.{appID}.v1.user.{userID}
	if m.nc != nil {
		// Drain any previous subscription before creating a new one.
		if oldSub, ok := m.subs.LoadAndDelete(key); ok {
			oldSub.(*nats.Subscription).Unsubscribe() //nolint:errcheck
		}
		sub, err := m.subscribeUser(appID, userID)
		if err != nil {
			m.log.Warnf("conn manager: nats subscribe user %s/%s: %v", appID, userID, err)
			// Non-fatal: connection proceeds without private messaging.
		} else {
			m.subs.Store(key, sub)
		}

		// Subscribe to control subject: nexus.{appID}.v1.ctrl.{userID}
		if oldCtrl, ok := m.ctrlSubs.LoadAndDelete(key); ok {
			oldCtrl.(*nats.Subscription).Unsubscribe() //nolint:errcheck
		}
		ctrlSub, err := m.subscribeControl(appID, userID)
		if err != nil {
			m.log.Warnf("conn manager: nats subscribe ctrl %s/%s: %v", appID, userID, err)
		} else {
			m.ctrlSubs.Store(key, ctrlSub)
		}
	}

	metrics.WSConnectionsActive.Inc()
	m.publishPresence(userID, "online")
	m.log.Infof("connected user %s/%s", appID, userID)
	return nil
}

// Disconnect removes the connection from local memory, unsubscribes from NATS,
// deletes Redis presence, and publishes an "offline" event. Safe to call multiple times.
func (m *ConnManager) Disconnect(ctx context.Context, appID, userID string) {
	key := connKey(appID, userID)

	// Run disconnect hooks before tearing down subscriptions.
	for _, hook := range m.disconnectHooks {
		hook(appID, userID)
	}

	// Unsubscribe from private NATS subject before removing the connection.
	if sub, ok := m.subs.LoadAndDelete(key); ok {
		if err := sub.(*nats.Subscription).Unsubscribe(); err != nil {
			m.log.Warnf("conn manager: nats unsubscribe user %s/%s: %v", appID, userID, err)
		}
	}
	// Unsubscribe from control NATS subject.
	if ctrlSub, ok := m.ctrlSubs.LoadAndDelete(key); ok {
		if err := ctrlSub.(*nats.Subscription).Unsubscribe(); err != nil {
			m.log.Warnf("conn manager: nats unsubscribe ctrl %s/%s: %v", appID, userID, err)
		}
	}
	m.writeMus.Delete(key)
	m.conns.Delete(key)

	if err := m.presence.DeleteUserPresence(ctx, appID, userID); err != nil {
		m.log.Warnf("conn manager: delete presence user %s/%s: %v", appID, userID, err)
	}

	metrics.WSConnectionsActive.Dec()
	m.publishPresence(userID, "offline")
	m.log.Infof("disconnected user %s/%s", appID, userID)
}

// RefreshPresence resets the Redis TTL for the user's presence key.
// Called on every heartbeat (Pong received from client). If the key has
// expired between heartbeats, it is recreated with a new TTL.
func (m *ConnManager) RefreshPresence(ctx context.Context, appID, userID string) {
	if err := m.presence.RefreshPresenceTTL(ctx, appID, userID, m.getPresenceTTL()); err != nil {
		// Key may have expired (e.g., brief Redis hiccup) — recreate it.
		m.log.Warnf("conn manager: refresh presence %s/%s: %v — re-setting", appID, userID, err)
		if err2 := m.presence.SetUserPresence(ctx, appID, userID, m.nodeID, m.getPresenceTTL()); err2 != nil {
			m.log.Errorf("conn manager: re-set presence %s/%s: %v", appID, userID, err2)
		}
	}
}

// Get returns the active connection for a user, if any.
func (m *ConnManager) Get(appID, userID string) (*websocket.Conn, bool) {
	v, ok := m.conns.Load(connKey(appID, userID))
	if !ok {
		return nil, false
	}
	return v.(*websocket.Conn), true
}

// CloseAll closes every active connection and drains all NATS subscriptions
// (used during graceful shutdown).
func (m *ConnManager) CloseAll() {
	m.subs.Range(func(_, v any) bool {
		v.(*nats.Subscription).Unsubscribe() //nolint:errcheck
		return true
	})
	m.ctrlSubs.Range(func(_, v any) bool {
		v.(*nats.Subscription).Unsubscribe() //nolint:errcheck
		return true
	})
	m.conns.Range(func(_, v any) bool {
		v.(*websocket.Conn).Close() //nolint:errcheck
		return true
	})
}

// subscribeUser creates a NATS subscription for nexus.{appID}.v1.user.{userID}.
// Each incoming message is forwarded to the user's active WebSocket connection.
// Writes are serialized via a per-connection mutex to satisfy gorilla's single-writer rule.
func (m *ConnManager) subscribeUser(appID, userID string) (*nats.Subscription, error) {
	subject := fmt.Sprintf("nexus.%s.v1.user.%s", appID, userID)
	key := connKey(appID, userID)

	sub, err := m.nc.Subscribe(subject, func(msg *nats.Msg) {
		defer infra.RecoverNATS("subscribeUser:" + key)
		conn, ok := m.Get(appID, userID)
		if !ok {
			return // user disconnected; subscription will be cleaned up shortly
		}
		mu := m.getWriteMu(key)
		mu.Lock()
		defer mu.Unlock()
		if err := conn.WriteMessage(websocket.TextMessage, msg.Data); err != nil {
			m.log.Warnf("conn manager: deliver to %s/%s: %v", appID, userID, err)
		}
	})
	if err != nil {
		return nil, err
	}
	m.applyPendingLimits(sub)
	return sub, nil
}

// subscribeControl creates a NATS subscription for nexus.{appID}.v1.ctrl.{userID}.
// Control messages from the HTTP Ingress tier (e.g., group join/leave) are dispatched
// to the registered ControlHandler.
func (m *ConnManager) subscribeControl(appID, userID string) (*nats.Subscription, error) {
	subject := fmt.Sprintf("nexus.%s.v1.ctrl.%s", appID, userID)
	sub, err := m.nc.Subscribe(subject, func(msg *nats.Msg) {
		defer infra.RecoverNATS("subscribeControl:" + appID + ":" + userID)
		if m.ctrlHandler != nil {
			m.ctrlHandler.HandleControl(appID, userID, msg.Data)
		}
	})
	if err != nil {
		return nil, err
	}
	m.applyPendingLimits(sub)
	return sub, nil
}

// applyPendingLimits sets NATS subscription pending limits if configured.
func (m *ConnManager) applyPendingLimits(sub *nats.Subscription) {
	if m.subPendingMsgsLimit > 0 && m.subPendingBytesLimit > 0 {
		sub.SetPendingLimits(m.subPendingMsgsLimit, m.subPendingBytesLimit) //nolint:errcheck
	}
}

// getWriteMu returns the write mutex for a connection key, creating it if needed.
// This serializes concurrent WriteMessage calls for the same connection.
func (m *ConnManager) getWriteMu(key string) *sync.Mutex {
	v, _ := m.writeMus.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// publishPresence publishes status ("online"/"offline") to NATS subject
// "presence.status.{userID}". No-ops when nc is nil.
func (m *ConnManager) publishPresence(userID, status string) {
	if m.nc == nil {
		return
	}
	subject := "presence.status." + userID
	if err := m.nc.Publish(subject, []byte(status)); err != nil {
		m.log.Warnf("conn manager: nats publish presence %s %s: %v", userID, status, err)
	}
}

// connKey returns the composite key "appID:userID" used for connection lookups.
func connKey(appID, userID string) string {
	return appID + ":" + userID
}
