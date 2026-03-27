package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// MessageStatusRepository implements domain.MessageStatusRepository for MySQL.
type MessageStatusRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewMessageStatusRepository creates a MessageStatusRepository backed by db.
func NewMessageStatusRepository(db *sql.DB) *MessageStatusRepository {
	return &MessageStatusRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// UpdateStatus upserts the delivery status for a message/user pair.
func (r *MessageStatusRepository) UpdateStatus(ctx context.Context, appID string, messageID uint64, userID string, status domain.MessageStatus) error {
	const q = `
		INSERT INTO message_status (message_id, app_id, user_id, status, updated_at)
		VALUES (?, ?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE status = VALUES(status), updated_at = NOW()`
	_, err := r.db.ExecContext(ctx, q, messageID, appID, userID, string(status))
	if err != nil {
		return fmt.Errorf("message_status_repo: update %d user %s: %w", messageID, userID, err)
	}
	return nil
}

// GetStatus returns the delivery status for a message/user pair, or nil if not found.
func (r *MessageStatusRepository) GetStatus(ctx context.Context, appID string, messageID uint64, userID string) (*domain.MessageStatusEntry, error) {
	const q = `
		SELECT message_id, app_id, user_id, status, updated_at
		FROM message_status
		WHERE message_id = ? AND app_id = ? AND user_id = ?`
	entry := &domain.MessageStatusEntry{}
	err := r.db.QueryRowContext(ctx, q, messageID, appID, userID).Scan(
		&entry.MessageID, &entry.AppID, &entry.UserID, &entry.Status, &entry.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("message_status_repo: get %d user %s: %w", messageID, userID, err)
	}
	return entry, nil
}
