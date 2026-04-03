package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// OutboxWriterRepository implements domain.OutboxWriter for MySQL.
// It inserts standalone outbox entries (data_type='event', data_id=NULL) without
// an associated message row — used by custom event dispatch.
type OutboxWriterRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewOutboxWriterRepository creates a new OutboxWriterRepository.
func NewOutboxWriterRepository(db *sql.DB) *OutboxWriterRepository {
	return &OutboxWriterRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// Save inserts a standalone outbox entry.
// For event rows, entry.DataID must be nil; the column is stored as NULL.
func (r *OutboxWriterRepository) Save(ctx context.Context, entry *domain.OutboxEntry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}

	const q = `
		INSERT INTO outbox (id, app_id, data_type, data_id, nats_subject, payload, status, retry_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', 0, ?)
	`
	_, err := r.db.ExecContext(ctx, q,
		entry.ID,
		entry.AppID,
		entry.DataType,
		entry.DataID, // nil → NULL
		entry.NATSSubject,
		entry.Payload,
		entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("outbox writer: insert: %w", err)
	}

	r.log.Debugf("Outbox entry saved: id=%d app_id=%s data_type=%s", entry.ID, entry.AppID, entry.DataType)
	return nil
}
