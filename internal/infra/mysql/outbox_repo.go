package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// OutboxRepository implements domain.OutboxRepository for MySQL.
type OutboxRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewOutboxRepository creates a new OutboxRepository.
func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// ProcessBatch fetches up to `limit` pending outbox entries using
// SELECT FOR UPDATE SKIP LOCKED (multi-instance safe), calls fn with
// the entire slice, then marks all rows published (fn == nil) or all
// rows failed (fn != nil) within the same transaction.
//
// Batch semantics mean the caller can publish all messages to NATS and
// issue a single FlushTimeout instead of one flush per message.
// Returns the number of rows processed and any fatal repository error.
func (r *OutboxRepository) ProcessBatch(ctx context.Context, limit int, fn func([]*domain.OutboxEntry) error) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("outbox: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — superseded by explicit Commit

	rows, err := tx.QueryContext(ctx, `
		SELECT id, app_id, data_type, data_id, nats_subject, payload, retry_count
		FROM outbox
		WHERE status = 'pending'
		ORDER BY created_at ASC
		LIMIT ?
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return 0, fmt.Errorf("outbox: fetch pending: %w", err)
	}

	var entries []*domain.OutboxEntry
	for rows.Next() {
		e := &domain.OutboxEntry{}
		if err := rows.Scan(&e.ID, &e.AppID, &e.DataType, &e.DataID, &e.NATSSubject, &e.Payload, &e.RetryCount); err != nil {
			rows.Close()
			return 0, fmt.Errorf("outbox: scan row: %w", err)
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("outbox: rows error: %w", err)
	}

	if len(entries) == 0 {
		return 0, tx.Commit()
	}

	// Call the batch publish function. On success all rows are marked published;
	// on error all rows are marked failed (conservative but safe for at-least-once).
	fnErr := fn(entries)

	for _, e := range entries {
		if fnErr != nil {
			r.log.Warnf("outbox: batch publish failed id=%d: %v", e.ID, fnErr)
			_, err = tx.ExecContext(ctx,
				`UPDATE outbox SET status='failed', retry_count=retry_count+1, last_error=? WHERE id=?`,
				fnErr.Error(), e.ID,
			)
		} else {
			now := time.Now().UTC()
			_, err = tx.ExecContext(ctx,
				`UPDATE outbox SET status='published', published_at=? WHERE id=?`,
				now, e.ID,
			)
		}
		if err != nil {
			return 0, fmt.Errorf("outbox: update status id=%d: %w", e.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("outbox: commit: %w", err)
	}

	return len(entries), nil
}
