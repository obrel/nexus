package egress

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	natsserver "github.com/nats-io/nats-server/v2/server"
	natstest "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/domain"
)

// mockPresenceRepo records calls for assertion.
type mockPresenceRepo struct {
	mu         sync.Mutex
	entries    map[string]string // "appID:userID" → nodeID
	setErr     error
	delErr     error
	refreshErr error
}

func newMockPresence() *mockPresenceRepo {
	return &mockPresenceRepo{entries: make(map[string]string)}
}

func (m *mockPresenceRepo) SetUserPresence(_ context.Context, appID, userID, nodeID string, _ int) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.mu.Lock()
	m.entries[appID+":"+userID] = nodeID
	m.mu.Unlock()
	return nil
}

func (m *mockPresenceRepo) GetUserPresence(_ context.Context, appID, userID string) (*domain.UserPresence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nodeID, ok := m.entries[appID+":"+userID]
	if !ok {
		return nil, nil
	}
	return &domain.UserPresence{UserID: userID, NodeID: nodeID, Status: "online"}, nil
}

func (m *mockPresenceRepo) DeleteUserPresence(_ context.Context, appID, userID string) error {
	if m.delErr != nil {
		return m.delErr
	}
	m.mu.Lock()
	delete(m.entries, appID+":"+userID)
	m.mu.Unlock()
	return nil
}

func (m *mockPresenceRepo) RefreshPresenceTTL(_ context.Context, _, _ string, _ int) error {
	return m.refreshErr
}

// dialTestServer creates an in-process WS server and returns a connected client conn.
func dialTestServer(t testing.TB) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConn := make(chan *websocket.Conn, 1)
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		serverConn <- conn
	}))
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	return <-serverConn, client
}

// --- Tests ---

func TestConnManager_Connect_SetsPresence(t *testing.T) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	if err := mgr.Connect(ctx, serverConn, "app1", "user1"); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	// Local map should have the connection.
	got, ok := mgr.Get("app1", "user1")
	if !ok || got != serverConn {
		t.Errorf("Get returned unexpected conn: ok=%v", ok)
	}

	// Presence should be set in Redis mock.
	presence.mu.Lock()
	nodeID := presence.entries["app1:user1"]
	presence.mu.Unlock()
	if nodeID != "node-1" {
		t.Errorf("presence nodeID = %q, want %q", nodeID, "node-1")
	}
}

func TestConnManager_Connect_PresenceError_RollsBack(t *testing.T) {
	presence := newMockPresence()
	presence.setErr = context.DeadlineExceeded
	mgr := NewConnManager(presence, nil, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer clientConn.Close()

	err := mgr.Connect(context.Background(), serverConn, "app1", "user1")
	if err == nil {
		t.Fatal("expected error from Connect, got nil")
	}

	// Connection must NOT be stored after rollback.
	if _, ok := mgr.Get("app1", "user1"); ok {
		t.Error("conn should not be in local map after presence failure")
	}
}

func TestConnManager_Disconnect_ClearsPresence(t *testing.T) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer clientConn.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, serverConn, "app1", "user1")
	mgr.Disconnect(ctx, "app1", "user1")

	if _, ok := mgr.Get("app1", "user1"); ok {
		t.Error("conn should be removed after Disconnect")
	}

	presence.mu.Lock()
	_, exists := presence.entries["app1:user1"]
	presence.mu.Unlock()
	if exists {
		t.Error("presence entry should be deleted after Disconnect")
	}
}

func TestConnManager_Disconnect_Idempotent(t *testing.T) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "node-1")
	ctx := context.Background()

	// Disconnect without prior Connect must not panic.
	mgr.Disconnect(ctx, "app1", "ghost")
}

func TestConnManager_Connect_ReplacesExisting(t *testing.T) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "node-1")
	ctx := context.Background()

	first, firstClient := dialTestServer(t)
	defer firstClient.Close()
	_ = mgr.Connect(ctx, first, "app1", "user1")

	second, secondClient := dialTestServer(t)
	defer secondClient.Close()
	_ = mgr.Connect(ctx, second, "app1", "user1")

	// Only second should remain.
	got, ok := mgr.Get("app1", "user1")
	if !ok || got != second {
		t.Errorf("expected second connection to be active; ok=%v, match=%v", ok, got == second)
	}
}

func TestConnManager_Get_UnknownUser(t *testing.T) {
	mgr := NewConnManager(newMockPresence(), nil, "node-1")
	if _, ok := mgr.Get("app1", "nobody"); ok {
		t.Error("Get should return false for unknown user")
	}
}

func TestConnManager_RefreshPresence_UpdatesTTL(t *testing.T) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "node-1")
	ctx := context.Background()

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	_ = mgr.Connect(ctx, serverConn, "app1", "user1")

	// RefreshPresence should not error when key exists.
	mgr.RefreshPresence(ctx, "app1", "user1")

	// Presence entry should still be there after refresh.
	if _, ok := presence.entries["app1:user1"]; !ok {
		t.Error("presence entry should exist after RefreshPresence")
	}
}

func TestConnManager_RefreshPresence_RecreateMissingKey(t *testing.T) {
	presence := newMockPresence()
	// Simulate key not found by making Expire fail → RefreshPresenceTTL returns error.
	presence.refreshErr = fmt.Errorf("key not found")
	mgr := NewConnManager(presence, nil, "node-1")
	ctx := context.Background()

	// RefreshPresence should fall back to SetUserPresence without panicking.
	mgr.RefreshPresence(ctx, "app1", "user1")
}

func TestPublishPresence_SkipsWhenNilConn(t *testing.T) {
	// ConnManager with nc=nil must not panic and must skip publishing.
	mgr := NewConnManager(newMockPresence(), nil, "node-1")
	ctx := context.Background()

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	// Neither Connect nor Disconnect should panic with nil NATS.
	if err := mgr.Connect(ctx, serverConn, "app1", "user1"); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	mgr.Disconnect(ctx, "app1", "user1")
}

func TestConnManager_CloseAll_DropsAllConns(t *testing.T) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "node-1")
	ctx := context.Background()

	srv1, cli1 := dialTestServer(t)
	srv2, cli2 := dialTestServer(t)
	defer cli1.Close()
	defer cli2.Close()

	_ = mgr.Connect(ctx, srv1, "app1", "userA")
	_ = mgr.Connect(ctx, srv2, "app1", "userB")

	mgr.CloseAll()

	// Both server-side conns are closed; clients see an error on next read.
	_, _, err1 := cli1.ReadMessage()
	_, _, err2 := cli2.ReadMessage()
	if err1 == nil || err2 == nil {
		t.Error("expected closed-connection errors on both clients after CloseAll")
	}
}

// --- Benchmarks ---

// BenchmarkConnect measures the overhead of registering a connection (sync.Map write + mock Redis).
func BenchmarkConnect(b *testing.B) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "bench-node")

	serverConn, clientConn := dialTestServer(b)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	b.ResetTimer()
	for i := range b.N {
		userID := fmt.Sprintf("u%d", i%100)
		_ = mgr.Connect(ctx, serverConn, "app1", userID)
	}
}

// BenchmarkConnManager_Get_10kConns measures O(1) lookup across a populated map.
func BenchmarkConnManager_Get_10kConns(b *testing.B) {
	const total = 10_000
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "bench-node")

	// Seed with one real connection reused under different keys.
	serverConn, clientConn := dialTestServer(b)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	for i := range total {
		mgr.conns.Store(fmt.Sprintf("app1:user%d", i), serverConn)
		_ = presence.SetUserPresence(ctx, "app1", fmt.Sprintf("user%d", i), "bench-node", 60)
	}

	b.ResetTimer()
	for i := range b.N {
		mgr.Get("app1", fmt.Sprintf("user%d", i%total))
	}
}

// BenchmarkHeartbeat_RefreshTTL measures the heartbeat path through ConnManager
// (mock Redis — no network I/O), isolating Go-side overhead.
func BenchmarkHeartbeat_RefreshTTL(b *testing.B) {
	presence := newMockPresence()
	mgr := NewConnManager(presence, nil, "bench-node")
	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		mgr.RefreshPresence(ctx, "app1", "user1")
	}
}

// ── NATS helpers ─────────────────────────────────────────────────────────────

// startTestNATS starts an in-process NATS server on a random port and returns
// a connected *nats.Conn. Both are cleaned up at test end.
func startTestNATS(t testing.TB) *nats.Conn {
	t.Helper()
	opts := &natsserver.Options{
		Host:           "127.0.0.1",
		Port:           -1, // OS-assigned port
		NoLog:          true,
		NoSigs:         true,
		MaxControlLine: 4096,
	}
	srv := natstest.RunServer(opts)
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// ── T-08: Private Messaging Pipeline tests ────────────────────────────────────

// TestConnManager_Connect_SubscribesToNATS verifies that connecting a user
// creates a NATS subscription for nexus.{appID}.v1.user.{userID}.
func TestConnManager_Connect_SubscribesToNATS(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	if err := mgr.Connect(ctx, serverConn, "app1", "user1"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	sub, ok := mgr.subs.Load(connKey("app1", "user1"))
	if !ok {
		t.Fatal("expected NATS subscription to be stored after Connect")
	}
	if !sub.(*nats.Subscription).IsValid() {
		t.Error("NATS subscription is not valid")
	}

	expected := "nexus.app1.v1.user.user1"
	if sub.(*nats.Subscription).Subject != expected {
		t.Errorf("subject = %q, want %q", sub.(*nats.Subscription).Subject, expected)
	}
}

// TestConnManager_Disconnect_UnsubscribesNATS verifies that disconnecting a user
// drains the NATS subscription and removes it from the manager.
func TestConnManager_Disconnect_UnsubscribesNATS(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer clientConn.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, serverConn, "app1", "user1")

	sub, _ := mgr.subs.Load(connKey("app1", "user1"))
	natsSub := sub.(*nats.Subscription)

	mgr.Disconnect(ctx, "app1", "user1")

	if _, ok := mgr.subs.Load(connKey("app1", "user1")); ok {
		t.Error("expected subscription to be removed after Disconnect")
	}
	if natsSub.IsValid() {
		t.Error("expected NATS subscription to be invalidated after Disconnect")
	}
}

// TestConnManager_NATSDeliver_PushesToWS is the core T-08 test: a message
// published to nexus.{appID}.v1.user.{userID} must arrive at the user's WS.
func TestConnManager_NATSDeliver_PushesToWS(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	if err := mgr.Connect(ctx, serverConn, "app1", "user1"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	payload := []byte(`{"type":"msg","text":"hello"}`)
	if err := nc.Publish("nexus.app1.v1.user.user1", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	nc.Flush() //nolint:errcheck

	clientConn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	_, got, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q, want %q", got, payload)
	}
}

// TestConnManager_NATSDeliver_NotDeliveredAfterDisconnect verifies that messages
// published after disconnect are not forwarded (subscription is gone).
func TestConnManager_NATSDeliver_NotDeliveredAfterDisconnect(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer clientConn.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, serverConn, "app1", "user1")
	mgr.Disconnect(ctx, "app1", "user1")

	// Any publish after unsubscribe must not reach the client.
	_ = nc.Publish("nexus.app1.v1.user.user1", []byte("ghost"))
	nc.Flush() //nolint:errcheck

	clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
	_, _, err := clientConn.ReadMessage()
	if err == nil {
		t.Error("expected no message to be delivered after disconnect")
	}
}

// TestConnManager_NATSDeliver_ReplacedConn verifies that when a user reconnects,
// messages go to the new connection and not the old one.
func TestConnManager_NATSDeliver_ReplacedConn(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")
	ctx := context.Background()

	// First connection.
	first, firstClient := dialTestServer(t)
	_ = mgr.Connect(ctx, first, "app1", "user1")

	// Second connection replaces the first.
	second, secondClient := dialTestServer(t)
	defer secondClient.Close()
	_ = mgr.Connect(ctx, second, "app1", "user1")

	payload := []byte(`{"text":"to new conn"}`)
	_ = nc.Publish("nexus.app1.v1.user.user1", payload)
	nc.Flush() //nolint:errcheck

	// New client receives the message.
	secondClient.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	_, got, err := secondClient.ReadMessage()
	if err != nil {
		t.Fatalf("secondClient ReadMessage: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload = %q, want %q", got, payload)
	}

	// Old client should NOT receive it (closed by Connect).
	firstClient.SetReadDeadline(time.Now().Add(50 * time.Millisecond)) //nolint:errcheck
	_, _, err = firstClient.ReadMessage()
	if err == nil {
		t.Error("expected old client to not receive the message after replacement")
	}
}

// TestConnManager_CloseAll_DrainsSubs verifies that CloseAll unsubscribes
// all NATS subscriptions in addition to closing WS connections.
func TestConnManager_CloseAll_DrainsSubs(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")
	ctx := context.Background()

	srv1, cli1 := dialTestServer(t)
	srv2, cli2 := dialTestServer(t)
	defer cli1.Close()
	defer cli2.Close()

	_ = mgr.Connect(ctx, srv1, "app1", "userA")
	_ = mgr.Connect(ctx, srv2, "app1", "userB")

	// Capture subscriptions before CloseAll.
	subA, _ := mgr.subs.Load(connKey("app1", "userA"))
	subB, _ := mgr.subs.Load(connKey("app1", "userB"))

	mgr.CloseAll()

	if subA.(*nats.Subscription).IsValid() {
		t.Error("userA subscription should be invalid after CloseAll")
	}
	if subB.(*nats.Subscription).IsValid() {
		t.Error("userB subscription should be invalid after CloseAll")
	}
}

// TestPushMessage_SerializedByMutex verifies that concurrent writes to the same
// connection are serialized by the per-connection write mutex (no panic/race).
func TestPushMessage_SerializedByMutex(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	if err := mgr.Connect(ctx, serverConn, "app1", "user1"); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	const writers = 20
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := range writers {
		go func(i int) {
			defer wg.Done()
			_ = nc.Publish("nexus.app1.v1.user.user1", []byte(fmt.Sprintf(`{"n":%d}`, i)))
			nc.Flush() //nolint:errcheck
		}(i)
	}
	wg.Wait()

	// Drain all messages delivered to the client (with a short deadline to stop).
	received := 0
	clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
	for {
		if _, _, err := clientConn.ReadMessage(); err != nil {
			break
		}
		received++
	}
	if received == 0 {
		t.Error("expected at least one message to be delivered")
	}
}

// TestGetWriteMu_ConcurrentCreation verifies that getWriteMu is safe under
// concurrent access and always returns the same mutex for the same key.
func TestGetWriteMu_ConcurrentCreation(t *testing.T) {
	mgr := NewConnManager(newMockPresence(), nil, "node-1")
	key := "app1:user1"

	const goroutines = 50
	results := make([]*sync.Mutex, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			results[i] = mgr.getWriteMu(key)
		}(i)
	}
	wg.Wait()

	// All goroutines must have received the exact same mutex pointer.
	for i := 1; i < goroutines; i++ {
		if results[i] != results[0] {
			t.Errorf("goroutine %d got a different mutex pointer", i)
		}
	}
}

// BenchmarkNATSDeliver measures end-to-end latency: NATS publish → WS write.
func BenchmarkNATSDeliver(b *testing.B) {
	nc := startTestNATS(b)
	mgr := NewConnManager(newMockPresence(), nc, "bench-node")
	ctx := context.Background()

	serverConn, clientConn := dialTestServer(b)
	defer serverConn.Close()
	defer clientConn.Close()

	_ = mgr.Connect(ctx, serverConn, "app1", "user1")

	payload := []byte(`{"text":"bench"}`)
	subject := "nexus.app1.v1.user.user1"

	b.ResetTimer()
	for range b.N {
		_ = nc.Publish(subject, payload)
		nc.Flush() //nolint:errcheck
		clientConn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
		_, _, _ = clientConn.ReadMessage()
	}
}

// BenchmarkConcurrentWrites_10Users measures write-mutex throughput across 10
// concurrent users sharing the same NATS connection.
func BenchmarkConcurrentWrites_10Users(b *testing.B) {
	nc := startTestNATS(b)
	mgr := NewConnManager(newMockPresence(), nc, "bench-node")
	ctx := context.Background()

	const users = 10
	clients := make([]*websocket.Conn, users)
	for i := range users {
		srv, cli := dialTestServer(b)
		clients[i] = cli
		userID := fmt.Sprintf("user%d", i)
		_ = mgr.Connect(ctx, srv, "app1", userID)
		// Drain delivered messages in background so writes don't block.
		go func(c *websocket.Conn) {
			for {
				c.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}(cli)
	}

	payload := []byte(`{"text":"bench"}`)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			subject := fmt.Sprintf("nexus.app1.v1.user.user%d", i%users)
			_ = nc.Publish(subject, payload)
			i++
		}
		nc.Flush() //nolint:errcheck
	})
}
