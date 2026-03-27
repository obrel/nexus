package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// PublishRepository implements domain.PublishRepository for MySQL.
type PublishRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewPublishRepository creates a new PublishRepository.
func NewPublishRepository(db *sql.DB) *PublishRepository {
	return &PublishRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// SaveTx saves a message and its corresponding outbox entry in a single transaction.
// This is the transactional outbox pattern: both inserts succeed atomically, or both fail.
func (r *PublishRepository) SaveTx(ctx context.Context, msg *domain.Message, outbox *domain.OutboxEntry) error {
	// Capture a single timestamp for the entire publish event
	now := time.Now().UTC()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	if outbox.CreatedAt.IsZero() {
		outbox.CreatedAt = now
	}

	// Begin transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Insert message
	const insertMessageSQL = `
		INSERT INTO messages (id, app_id, shard, recipient_type, recipient_id, sender_type, sender_id, content, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, insertMessageSQL,
		msg.ID,
		msg.AppID,
		msg.ShardID,
		msg.RecipientType, // 'group' or 'user'
		msg.RecipientID,   // group ID or user ID depending on type
		msg.SenderType,    // sender type ('user' for now)
		msg.SenderID,      // sender identifier
		string(msg.Content), // content (JSON) in messages table
		domain.StatusSent, // default status
		msg.CreatedAt,
		msg.CreatedAt,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			r.log.Errorf("Rollback failed after message insert: %v", rbErr)
		}
		return fmt.Errorf("failed to insert message: %w", err)
	}

	// Insert outbox entry
	const insertOutboxSQL = `
		INSERT INTO outbox (id, app_id, message_id, nats_subject, payload, status, retry_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, insertOutboxSQL,
		outbox.ID,
		outbox.AppID,
		outbox.MessageID,
		outbox.NATSSubject,
		outbox.Payload,    // message body for NATS publish
		outbox.Status,     // should be "pending"
		0,                 // retry_count
		outbox.CreatedAt,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			r.log.Errorf("Rollback failed after outbox insert: %v", rbErr)
		}
		return fmt.Errorf("failed to insert outbox: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	r.log.Debugf("Published message: id=%d app_id=%s shard=%d recipient_type=%s recipient_id=%s",
		msg.ID, msg.AppID, msg.ShardID, msg.RecipientType, msg.RecipientID)

	return nil
}
