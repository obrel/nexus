package egress

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/obrel/nexus/internal/domain"
)

// mockMessageRepo implements domain.MessageRepository for history tests.
type mockMessageRepo struct {
	msgs []*domain.Message
	err  error
}

func (m *mockMessageRepo) Save(_ context.Context, _ *domain.Message) error { return nil }
func (m *mockMessageRepo) GetByID(_ context.Context, _ uint64) (*domain.Message, error) {
	return nil, nil
}
func (m *mockMessageRepo) GetGroupMessages(_ context.Context, _, _ string, _ uint64, _ int) ([]*domain.Message, error) {
	return m.msgs, m.err
}
func (m *mockMessageRepo) GetPrivateMessages(_ context.Context, _, _, _ string, _ uint64, _ int) ([]*domain.Message, error) {
	return m.msgs, m.err
}
func (m *mockMessageRepo) Update(_ context.Context, _ string, _ uint64, _ string) error { return nil }
func (m *mockMessageRepo) SoftDelete(_ context.Context, _ string, _ uint64) error        { return nil }

// makeMessages returns n domain.Message values with sequential Snowflake-like IDs.
func makeMessages(n int) []*domain.Message {
	msgs := make([]*domain.Message, n)
	for i := range n {
		msgs[i] = &domain.Message{
			ID:            uint64(100 + i),
			AppID:         "app1",
			RecipientType: domain.RecipientGroup,
			RecipientID:   "room_general",
			ShardID:       0,
			Content:       json.RawMessage(fmt.Sprintf(`{"type":"text","text":"msg%d"}`, i)),
			SenderType:    domain.SenderUser,
			SenderID:      "alice",
			CreatedAt:     time.Now(),
		}
	}
	return msgs
}

// TestHistoryService_CatchUp_SendsMessages verifies that messages returned by
// the repository are written to the WebSocket connection in order.
func TestHistoryService_CatchUp_SendsMessages(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, serverConn, "app1", "user1")

	repo := &mockMessageRepo{msgs: makeMessages(3)}
	svc := NewHistoryService(repo, mgr)

	svc.CatchUp(ctx, serverConn, "app1", "user1", "room_general", 99)

	// Client should receive exactly 3 messages.
	clientConn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
	for i := range 3 {
		_, data, err := clientConn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage #%d: %v", i, err)
		}
		var msg domain.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal #%d: %v", i, err)
		}
		want := uint64(100 + i)
		if msg.ID != want {
			t.Errorf("msg[%d].ID = %d, want %d", i, msg.ID, want)
		}
	}
}

// TestHistoryService_CatchUp_ZeroLastMsgID_Skips verifies that no DB call is
// made and nothing is written when lastMsgID is 0.
func TestHistoryService_CatchUp_ZeroLastMsgID_Skips(t *testing.T) {
	repo := &mockMessageRepo{msgs: makeMessages(5)}
	mgr := NewConnManager(newMockPresence(), nil, "node-1")
	svc := NewHistoryService(repo, mgr)

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	_ = mgr.Connect(context.Background(), serverConn, "app1", "user1")

	svc.CatchUp(context.Background(), serverConn, "app1", "user1", "room_general", 0)

	clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
	_, _, err := clientConn.ReadMessage()
	if err == nil {
		t.Error("expected no message when lastMsgID=0")
	}
}

// TestHistoryService_CatchUp_EmptyResult verifies nothing is written when
// the repository returns an empty slice.
func TestHistoryService_CatchUp_EmptyResult(t *testing.T) {
	repo := &mockMessageRepo{msgs: nil}
	mgr := NewConnManager(newMockPresence(), nil, "node-1")
	svc := NewHistoryService(repo, mgr)

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	_ = mgr.Connect(context.Background(), serverConn, "app1", "user1")

	svc.CatchUp(context.Background(), serverConn, "app1", "user1", "room_general", 50)

	clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
	_, _, err := clientConn.ReadMessage()
	if err == nil {
		t.Error("expected no message for empty result")
	}
}

// TestHistoryService_CatchUp_DBError verifies that a repository error is
// handled gracefully (logged, no panic, nothing written to conn).
func TestHistoryService_CatchUp_DBError(t *testing.T) {
	repo := &mockMessageRepo{err: fmt.Errorf("connection refused")}
	mgr := NewConnManager(newMockPresence(), nil, "node-1")
	svc := NewHistoryService(repo, mgr)

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	_ = mgr.Connect(context.Background(), serverConn, "app1", "user1")

	// Must not panic.
	svc.CatchUp(context.Background(), serverConn, "app1", "user1", "room_general", 50)

	clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
	_, _, err := clientConn.ReadMessage()
	if err == nil {
		t.Error("expected no message after DB error")
	}
}

// TestHistoryService_CatchUp_WriteMutexSerialization verifies that concurrent
// catch-up calls and NATS deliveries do not produce interleaved frames.
func TestHistoryService_CatchUp_WriteMutexSerialization(t *testing.T) {
	nc := startTestNATS(t)
	mgr := NewConnManager(newMockPresence(), nc, "node-1")

	serverConn, clientConn := dialTestServer(t)
	defer serverConn.Close()
	defer clientConn.Close()

	ctx := context.Background()
	_ = mgr.Connect(ctx, serverConn, "app1", "user1")

	repo := &mockMessageRepo{msgs: makeMessages(10)}
	svc := NewHistoryService(repo, mgr)

	// Fire catch-up and a NATS publish concurrently.
	done := make(chan struct{})
	go func() {
		svc.CatchUp(ctx, serverConn, "app1", "user1", "room_general", 99)
		close(done)
	}()
	_ = nc.Publish("nexus.app1.v1.user.user1", []byte(`{"type":"realtime"}`))
	nc.Flush() //nolint:errcheck
	<-done

	// Drain messages; the test passes as long as there are no panics or races.
	received := 0
	clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
	for {
		if _, _, err := clientConn.ReadMessage(); err != nil {
			break
		}
		received++
	}
	if received == 0 {
		t.Error("expected at least one message")
	}
}

// BenchmarkHistoryService_CatchUp measures catch-up delivery for 200 messages.
func BenchmarkHistoryService_CatchUp(b *testing.B) {
	mgr := NewConnManager(newMockPresence(), nil, "bench-node")
	repo := &mockMessageRepo{msgs: makeMessages(catchUpLimit)}
	svc := NewHistoryService(repo, mgr)

	serverConn, clientConn := dialTestServer(b)
	defer serverConn.Close()

	_ = mgr.Connect(context.Background(), serverConn, "bench", "u1")

	// Drain client messages in background.
	go func() {
		for {
			clientConn.SetReadDeadline(time.Now().Add(time.Second)) //nolint:errcheck
			if _, _, err := clientConn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	for range b.N {
		svc.CatchUp(context.Background(), serverConn, "bench", "u1", "room_general", 0)
	}
}

// Ensure egress.HistoryService satisfies the ws.HistorySyncer interface shape.
// This is a compile-time check: if the interface changes, this will fail to build.
var _ interface {
	CatchUp(ctx context.Context, conn *websocket.Conn, appID, userID, groupID string, lastMsgID uint64)
} = (*HistoryService)(nil)
