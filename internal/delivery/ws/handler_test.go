package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/egress"
)

const testSecret = "test-secret"

// mockPresenceRepo is a minimal in-memory presence repo for handler tests.
type mockPresenceRepo struct{}

func (m *mockPresenceRepo) SetUserPresence(_ context.Context, _, _, _ string, _ int) error {
	return nil
}
func (m *mockPresenceRepo) GetUserPresence(_ context.Context, _, _ string) (*domain.UserPresence, error) {
	return nil, nil
}
func (m *mockPresenceRepo) DeleteUserPresence(_ context.Context, _, _ string) error { return nil }
func (m *mockPresenceRepo) RefreshPresenceTTL(_ context.Context, _, _ string, _ int) error {
	return nil
}

func makeHandler() *Handler {
	mgr := egress.NewConnManager(&mockPresenceRepo{}, nil, "test-node")
	return NewHandler(testSecret, mgr, nil, nil, &config.WSConfig{})
}

func makeToken(sub, tid string) string {
	claims := &Claims{
		Sub: sub,
		Tid: tid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	return token
}

func dialServer(t *testing.T, srv *httptest.Server, protocols []string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	dialer := websocket.Dialer{Subprotocols: protocols}
	return dialer.Dial(url, nil)
}

func TestHandler_MissingSubProtocol(t *testing.T) {
	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	_, resp, err := dialServer(t, srv, nil)
	if err == nil {
		t.Fatal("expected dial to fail with missing protocol")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandler_InvalidJWT(t *testing.T) {
	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	_, resp, err := dialServer(t, srv, []string{wsProtocol, "not.a.jwt"})
	if err == nil {
		t.Fatal("expected dial to fail with invalid JWT")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandler_ValidJWT_Connects(t *testing.T) {
	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	token := makeToken("user1", "app1")
	conn, _, err := dialServer(t, srv, []string{wsProtocol, token})
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Client can send a message without error.
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Errorf("WriteMessage error: %v", err)
	}
}

func TestExtractToken(t *testing.T) {
	cases := []struct {
		protocols []string
		wantOK    bool
		wantToken string
	}{
		{[]string{wsProtocol, "mytoken"}, true, "mytoken"},
		{[]string{"mytoken", wsProtocol}, true, "mytoken"},
		{[]string{wsProtocol}, false, ""},
		{[]string{"mytoken"}, false, ""},
		{nil, false, ""},
	}

	for _, tc := range cases {
		tok, ok := extractToken(tc.protocols)
		if ok != tc.wantOK || tok != tc.wantToken {
			t.Errorf("extractToken(%v) = (%q, %v), want (%q, %v)",
				tc.protocols, tok, ok, tc.wantToken, tc.wantOK)
		}
	}
}

func TestHandler_JWT_MissingClaims(t *testing.T) {
	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	// Token with empty sub (userID missing).
	claims := &Claims{
		Sub: "",
		Tid: "app1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))

	_, resp, err := dialServer(t, srv, []string{wsProtocol, token})
	if err == nil {
		t.Fatal("expected dial to fail with missing claims")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandler_ReadLoop_GracefulClose(t *testing.T) {
	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	token := makeToken("user2", "app1")
	conn, _, err := dialServer(t, srv, []string{wsProtocol, token})
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	// Client initiates a clean close; server readLoop should return without panic.
	if err := conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		t.Fatalf("WriteMessage close error: %v", err)
	}
	conn.Close()
}

// trackingPresenceRepo counts RefreshPresenceTTL calls for heartbeat assertions.
// refreshCalls uses atomic to avoid data races (server and test goroutines access it concurrently).
type trackingPresenceRepo struct {
	mockPresenceRepo
	refreshCalls atomic.Int32
}

func (m *trackingPresenceRepo) RefreshPresenceTTL(_ context.Context, _, _ string, _ int) error {
	m.refreshCalls.Add(1)
	return nil
}

// makeHandlerFast returns a Handler with very short timings for test speed.
func makeHandlerFast(repo *trackingPresenceRepo) (*Handler, *egress.ConnManager) {
	mgr := egress.NewConnManager(repo, nil, "test-node")
	h := NewHandler(testSecret, mgr, nil, nil, &config.WSConfig{})
	h.pingPeriod = 20 * time.Millisecond
	h.pongWait = 60 * time.Millisecond
	h.writeWait = 20 * time.Millisecond
	return h, mgr
}

func TestReadLoop_PingHandler_RefreshesPresence(t *testing.T) {
	repo := &trackingPresenceRepo{}
	h, _ := makeHandlerFast(repo)
	srv := httptest.NewServer(h)
	defer srv.Close()

	token := makeToken("user1", "app1")
	conn, _, err := dialServer(t, srv, []string{wsProtocol, token})
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Send a WebSocket PING control frame from the client.
	if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("WriteControl PING: %v", err)
	}

	// Give the server time to process the PING and call RefreshPresence.
	time.Sleep(50 * time.Millisecond)

	if repo.refreshCalls.Load() == 0 {
		t.Error("expected RefreshPresenceTTL to be called after client PING")
	}
}

func TestPingLoop_SendsPingPeriodically(t *testing.T) {
	h, _ := makeHandlerFast(&trackingPresenceRepo{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	token := makeToken("user1", "app1")

	pingReceived := make(chan struct{}, 1)
	dialer := websocket.Dialer{
		Subprotocols: []string{wsProtocol, token},
	}
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	conn.SetPingHandler(func(string) error {
		select {
		case pingReceived <- struct{}{}:
		default:
		}
		return conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second))
	})

	// Drain messages so the PingHandler fires.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-pingReceived:
		// success
	case <-time.After(200 * time.Millisecond):
		t.Error("expected to receive a PING from server within 200ms")
	}
}

func TestReadLoop_DeadlineExceeded_Disconnects(t *testing.T) {
	h, mgr := makeHandlerFast(&trackingPresenceRepo{})
	srv := httptest.NewServer(h)
	defer srv.Close()

	token := makeToken("user1", "app1")
	conn, _, err := dialServer(t, srv, []string{wsProtocol, token})
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	// Override client-side ping handler to suppress auto-PONG so the server
	// never receives a PONG — read deadline should expire and server should clean up.
	conn.SetPingHandler(func(string) error { return nil })

	// Drain messages so the ping handler fires and suppression takes effect.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Wait longer than pongWait (60ms) — server should have removed the connection.
	time.Sleep(200 * time.Millisecond)

	if _, ok := mgr.Get("app1", "user1"); ok {
		t.Error("expected server to remove connection after pongWait, but it is still registered")
	}
}

// --- Benchmarks ---

func BenchmarkJWT_Parse(b *testing.B) {
	token := makeToken("user1", "app1")
	h := makeHandler()

	b.ResetTimer()
	for range b.N {
		_, _, err := h.parseJWT(token)
		if err != nil {
			b.Fatalf("parseJWT error: %v", err)
		}
	}
}

func BenchmarkHandler_ValidJWT(b *testing.B) {
	srv := httptest.NewServer(makeHandler())
	defer srv.Close()

	token := makeToken("user1", "app1")
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	dialer := websocket.Dialer{Subprotocols: []string{wsProtocol, token}}

	b.ResetTimer()
	for range b.N {
		conn, _, err := dialer.Dial(url, nil)
		if err != nil {
			b.Fatalf("dial error: %v", err)
		}
		conn.Close()
	}
}

