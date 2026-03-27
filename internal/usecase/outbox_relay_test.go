package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	natspkg "github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/circuitbreaker"
	"github.com/obrel/nexus/internal/infra/logger"
)

// mockOutboxRepository records ProcessBatch calls for assertion.
type mockOutboxRepository struct {
	entries   []*domain.OutboxEntry
	returnErr error
	callCount int
	lastFnErr error
}

func (m *mockOutboxRepository) ProcessBatch(
	ctx context.Context,
	limit int,
	fn func([]*domain.OutboxEntry) error,
) (int, error) {
	if m.returnErr != nil {
		return 0, m.returnErr
	}
	m.callCount++
	if len(m.entries) == 0 {
		return 0, nil
	}
	batch := m.entries
	if len(batch) > limit {
		batch = batch[:limit]
	}
	m.lastFnErr = fn(batch)
	return len(batch), nil
}

// --- constructor tests ---

func TestNewOutboxRelayWorker_NilRepo(t *testing.T) {
	var nc *natspkg.Conn
	_, err := NewOutboxRelayWorker(nil, nc, 0, 0)
	if err == nil {
		t.Fatal("expected error for nil repo, got nil")
	}
	if err.Error() != "outbox relay: repo is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewOutboxRelayWorker_NilConn(t *testing.T) {
	repo := &mockOutboxRepository{}
	var nc *natspkg.Conn
	_, err := NewOutboxRelayWorker(repo, nc, 0, 0)
	if err == nil {
		t.Fatal("expected error for nil nats conn, got nil")
	}
	if err.Error() != "outbox relay: nats connection is required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- Run tests ---

func TestRun_GracefulShutdown(t *testing.T) {
	repo := &mockOutboxRepository{} // always returns 0 entries → nc is never called
	w := &OutboxRelayWorker{
		repo:         repo,
		nc:           nil,
		cb:           circuitbreaker.New(5, 10*time.Second),
		pollInterval: 10 * time.Millisecond,
		batchSize:    50,
		log:          logger.For("test", "outbox_relay"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRun_ContinuesOnBatchError(t *testing.T) {
	repo := &mockOutboxRepository{returnErr: errors.New("db down")}
	w := &OutboxRelayWorker{
		repo:         repo,
		nc:           nil,
		cb:           circuitbreaker.New(5, 10*time.Second),
		pollInterval: 10 * time.Millisecond,
		batchSize:    50,
		log:          logger.For("test", "outbox_relay"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// --- Benchmark ---

// BenchmarkPublishBatch_MockRepo benchmarks the repo dispatch + batch loop
// without real NATS I/O, isolating the Go-side overhead.
func BenchmarkPublishBatch_MockRepo(b *testing.B) {
	entries := make([]*domain.OutboxEntry, 50)
	for i := range entries {
		entries[i] = &domain.OutboxEntry{
			ID:          uint64(i + 1),
			NATSSubject: "nexus.app.v1.grp.1.room_bench",
			Payload:     `{"content":"hello"}`,
		}
	}
	repo := &mockOutboxRepository{entries: entries}
	ctx := context.Background()
	noopFn := func(_ []*domain.OutboxEntry) error { return nil }

	b.ResetTimer()
	for range b.N {
		_, _ = repo.ProcessBatch(ctx, 50, noopFn)
	}
}
