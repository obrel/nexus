package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// MessageRepository implements domain.MessageRepository for MySQL.
type MessageRepository struct {
	db  *sql.DB
	log logger.Logger
}

// NewMessageRepository creates a MessageRepository backed by db.
func NewMessageRepository(db *sql.DB) *MessageRepository {
	return &MessageRepository{
		db:  db,
		log: logger.For("infra", "mysql"),
	}
}

// Save inserts a message row. ID must be set by the caller (Snowflake).
func (r *MessageRepository) Save(ctx context.Context, msg *domain.Message) error {
	const q = `
		INSERT INTO messages (id, app_id, shard, recipient_type, recipient_id, sender_type, sender_id, content, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		msg.ID, msg.AppID, msg.ShardID, msg.RecipientType, msg.RecipientID,
		msg.SenderType, msg.SenderID, string(msg.Content), domain.StatusSent, msg.CreatedAt, msg.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("message_repo: save: %w", err)
	}
	return nil
}

// GetByID fetches a single message by its Snowflake ID.
func (r *MessageRepository) GetByID(ctx context.Context, id uint64) (*domain.Message, error) {
	const q = `
		SELECT id, app_id, shard, recipient_type, recipient_id, sender_type, sender_id, content, created_at
		FROM messages WHERE id = ?`
	row := r.db.QueryRowContext(ctx, q, id)
	msg := &domain.Message{}
	if err := row.Scan(&msg.ID, &msg.AppID, &msg.ShardID, &msg.RecipientType, &msg.RecipientID, &msg.SenderType, &msg.SenderID, &msg.Content, &msg.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("message_repo: get_by_id %d: %w", id, err)
	}
	return msg, nil
}

// Update modifies the content of an existing message. Only app-scoped updates are allowed.
func (r *MessageRepository) Update(ctx context.Context, appID string, msgID uint64, content string) error {
	const q = `UPDATE messages SET content = ?, updated_at = NOW() WHERE id = ? AND app_id = ?`
	res, err := r.db.ExecContext(ctx, q, content, msgID, appID)
	if err != nil {
		return fmt.Errorf("message_repo: update %d: %w", msgID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("message_repo: update %d: not found", msgID)
	}
	return nil
}

// SoftDelete marks a message as deleted by setting status to 'deleted'.
func (r *MessageRepository) SoftDelete(ctx context.Context, appID string, msgID uint64) error {
	const q = `UPDATE messages SET status = 'deleted', updated_at = NOW() WHERE id = ? AND app_id = ?`
	res, err := r.db.ExecContext(ctx, q, msgID, appID)
	if err != nil {
		return fmt.Errorf("message_repo: soft_delete %d: %w", msgID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("message_repo: soft_delete %d: not found", msgID)
	}
	return nil
}

// GetGroupMessages returns up to limit group messages for the given group where id > sinceID,
// ordered ascending by id.
func (r *MessageRepository) GetGroupMessages(ctx context.Context, appID, groupID string, sinceID uint64, limit int) ([]*domain.Message, error) {
	const q = `
		SELECT id, app_id, shard, recipient_type, recipient_id, sender_type, sender_id, content, created_at
		FROM messages
		WHERE app_id = ? AND recipient_type = 'group' AND recipient_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?`
	return r.scanMessages(ctx, q, appID, groupID, sinceID, limit)
}

// GetPrivateMessages returns up to limit messages in the private conversation between
// userA and userB (bidirectional), ordered ascending by id.
func (r *MessageRepository) GetPrivateMessages(ctx context.Context, appID, userA, userB string, sinceID uint64, limit int) ([]*domain.Message, error) {
	const q = `
		SELECT id, app_id, shard, recipient_type, recipient_id, sender_type, sender_id, content, created_at
		FROM messages
		WHERE app_id = ? AND recipient_type = 'user'
		  AND ((sender_id = ? AND recipient_id = ?) OR (sender_id = ? AND recipient_id = ?))
		  AND id > ?
		ORDER BY id ASC
		LIMIT ?`
	rows, err := r.db.QueryContext(ctx, q, appID, userA, userB, userB, userA, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("message_repo: get_private %s/%s<->%s since %d: %w", appID, userA, userB, sinceID, err)
	}
	defer rows.Close()
	return r.collectMessages(rows)
}

// scanMessages executes a query with (appID, recipientID, sinceID, limit) params and scans results.
func (r *MessageRepository) scanMessages(ctx context.Context, q, appID, recipientID string, sinceID uint64, limit int) ([]*domain.Message, error) {
	rows, err := r.db.QueryContext(ctx, q, appID, recipientID, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("message_repo: query: %w", err)
	}
	defer rows.Close()
	return r.collectMessages(rows)
}

// collectMessages scans rows into Message slices.
func (r *MessageRepository) collectMessages(rows *sql.Rows) ([]*domain.Message, error) {
	var msgs []*domain.Message
	for rows.Next() {
		msg := &domain.Message{}
		if err := rows.Scan(&msg.ID, &msg.AppID, &msg.ShardID, &msg.RecipientType, &msg.RecipientID, &msg.SenderType, &msg.SenderID, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("message_repo: scan: %w", err)
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("message_repo: rows: %w", err)
	}
	return msgs, nil
}
