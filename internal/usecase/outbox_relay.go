package usecase

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"runtime/debug"

	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/circuitbreaker"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
)

const (
	// natsFlushTimeout is the maximum wait for NATS server ACK after publishing a batch.
	natsFlushTimeout = 5 * time.Second
	// maxBackoff caps the exponential backoff duration.
	maxBackoff = 30 * time.Second
	// baseBackoff is the starting backoff duration after a failure.
	baseBackoff = 500 * time.Millisecond
)

// OutboxRelayWorker polls the outbox table and publishes pending messages to NATS.
//
// At-least-once delivery guarantee:
//   - All messages in a batch are published with nc.Publish (buffered, non-blocking).
//   - A single nc.FlushTimeout waits for NATS server ACK for the whole batch.
//   - Only after a successful flush does the repository mark all rows 'published'.
//   - If flush fails, all rows in the batch are marked 'failed' and retried next poll.
//   - If the worker crashes before the commit, rows remain 'pending' for the next cycle.
//
// Multi-instance safety is provided by SELECT FOR UPDATE SKIP LOCKED in the repository.
type OutboxRelayWorker struct {
	repo         domain.OutboxRepository
	nc           *nats.Conn
	cb           *circuitbreaker.Breaker
	pollInterval time.Duration
	batchSize    int
	log          logger.Logger
}

// NewOutboxRelayWorker creates a new OutboxRelayWorker.
// Returns an error if any required dependency is nil.
func NewOutboxRelayWorker(repo domain.OutboxRepository, nc *nats.Conn, batchSize int, pollIntervalMs int) (*OutboxRelayWorker, error) {
	if repo == nil {
		return nil, fmt.Errorf("outbox relay: repo is required")
	}
	if nc == nil {
		return nil, fmt.Errorf("outbox relay: nats connection is required")
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	pollInterval := time.Duration(pollIntervalMs) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	return &OutboxRelayWorker{
		repo:         repo,
		nc:           nc,
		cb:           circuitbreaker.New(5, 10*time.Second),
		pollInterval: pollInterval,
		batchSize:    batchSize,
		log:          logger.For("usecase", "outbox_relay"),
	}, nil
}

// Run starts the relay loop, blocking until ctx is cancelled.
// Uses exponential backoff with jitter on consecutive failures.
func (w *OutboxRelayWorker) Run(ctx context.Context) error {
	w.log.Infof("Outbox relay started (batch=%d interval=%s)", w.batchSize, w.pollInterval)

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	var consecutiveFailures int

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Outbox relay stopping")
			return ctx.Err()
		case <-ticker.C:
			n, err := w.processBatchSafe(ctx)
			if err != nil {
				consecutiveFailures++
				metrics.OutboxRelayErrors.Inc()
				w.log.Errorf("ProcessBatch error: %v", err)

				// Exponential backoff with jitter before next attempt.
				backoff := w.calcBackoff(consecutiveFailures)
				w.log.Infof("Backing off %s after %d consecutive failures", backoff, consecutiveFailures)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoff):
				}
				continue
			}

			consecutiveFailures = 0
			if n > 0 {
				metrics.OutboxRelayedTotal.Add(float64(n))
				metrics.OutboxPendingGauge.Set(float64(n))
				w.log.Infof("Relayed %d outbox entries", n)
			} else {
				metrics.OutboxPendingGauge.Set(0)
			}
		}
	}
}

// calcBackoff returns an exponential backoff duration with jitter.
// Formula: min(maxBackoff, baseBackoff * 2^failures) * (0.5 + rand(0, 0.5))
func (w *OutboxRelayWorker) calcBackoff(failures int) time.Duration {
	exp := math.Pow(2, float64(failures))
	backoff := time.Duration(float64(baseBackoff) * exp)
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	// Add jitter: 50-100% of calculated backoff.
	jitter := 0.5 + rand.Float64()*0.5
	return time.Duration(float64(backoff) * jitter)
}

// processBatchSafe wraps ProcessBatch with panic recovery so that a single
// corrupt entry or unexpected nil does not crash the entire worker process.
func (w *OutboxRelayWorker) processBatchSafe(ctx context.Context) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			w.log.Errorf("Panic in outbox relay: %v\n%s", r, debug.Stack())
			metrics.NATSPanicRecoveries.Inc()
			err = fmt.Errorf("panic recovered: %v", r)
		}
	}()

	// Use circuit breaker to avoid hammering a down dependency.
	var result int
	cbErr := w.cb.Execute(func() error {
		var innerErr error
		result, innerErr = w.repo.ProcessBatch(ctx, w.batchSize, w.publishBatch)
		return innerErr
	})
	return result, cbErr
}

// publishBatch publishes all entries with nc.Publish (buffered), then issues a
// single FlushTimeout for the whole batch. This is more efficient than flushing
// after each message: O(1) round-trips to NATS instead of O(n).
//
// If any individual Publish call fails, the batch is aborted and the error is
// returned — the repository will mark all rows in the batch as 'failed'.
func (w *OutboxRelayWorker) publishBatch(entries []*domain.OutboxEntry) error {
	for _, e := range entries {
		if err := w.nc.Publish(e.NATSSubject, []byte(e.Payload)); err != nil {
			return fmt.Errorf("nats publish id=%d: %w", e.ID, err)
		}
	}
	// Single flush — waits for NATS server to ACK all buffered messages.
	if err := w.nc.FlushTimeout(natsFlushTimeout); err != nil {
		return fmt.Errorf("nats flush: %w", err)
	}
	return nil
}
