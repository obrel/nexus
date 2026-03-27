package domain

import (
	"encoding/json"
	"time"
)

// MessageStatus defines the delivery state of a message for a specific user.
type MessageStatus string

const (
	// StatusSent indicates the message has been persisted but not yet delivered.
	StatusSent MessageStatus = "sent"
	// StatusReceived indicates the message was delivered to the recipient's device.
	StatusReceived MessageStatus = "received"
	// StatusRead indicates the recipient has read the message.
	StatusRead MessageStatus = "read"
)

// RecipientType distinguishes group messages from private (direct) messages.
type RecipientType string

const (
	// RecipientGroup targets a group — delivered via shard-based NATS subject.
	RecipientGroup RecipientType = "group"
	// RecipientUser targets a specific user — delivered via private NATS subject.
	RecipientUser RecipientType = "user"
)

// SenderType identifies the kind of entity that sent a message.
type SenderType string

const (
	// SenderUser indicates the message was sent by a human user.
	SenderUser SenderType = "user"
)

// Message represents a chat message stored in the system.
type Message struct {
	ID            uint64          `json:"id"`
	AppID         string          `json:"app_id"`
	RecipientType RecipientType   `json:"recipient_type"`
	RecipientID   string          `json:"recipient_id"`
	ShardID       int             `json:"shard_id"`
	Content       json.RawMessage `json:"content"`
	SenderType    SenderType      `json:"sender_type"`
	SenderID      string          `json:"sender_id"`
	CreatedAt     time.Time       `json:"created_at"`
}

// IsPrivate returns true if this message targets a specific user (DM).
func (m *Message) IsPrivate() bool {
	return m.RecipientType == RecipientUser
}

// OutboxEntry represents a message queued for publishing to NATS.
type OutboxEntry struct {
	ID          uint64     `json:"id"`
	AppID       string     `json:"app_id"`
	MessageID   uint64     `json:"message_id"`
	NATSSubject string     `json:"nats_subject"`
	Payload     string     `json:"payload"` // serialised message body published to NATS
	Status      string     `json:"status"`  // pending | published | failed
	RetryCount  int        `json:"retry_count"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// Group represents a chat group/channel.
type Group struct {
	ID          string    `json:"group_id"`
	AppID       string    `json:"app_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ShardID     int       `json:"shard_id"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// UserPresence tracks the online status and connected node of a user.
type UserPresence struct {
	UserID string `json:"user_id"`
	Status string `json:"status"` // online | offline
	NodeID string `json:"node_id"`
}

// UserProfile represents a user record from the `users` table.
type UserProfile struct {
	ID           string    `json:"user_id"`
	AppID        string    `json:"app_id"`
	Email        string    `json:"email,omitempty"`
	Name         string    `json:"name"`
	Bio          string    `json:"bio"`
	PrivateTopic string    `json:"private_topic"`
	CreatedAt    time.Time `json:"created_at"`
}

// MessageStatusEntry tracks per-user delivery state for a message.
type MessageStatusEntry struct {
	MessageID uint64        `json:"message_id"`
	AppID     string        `json:"app_id"`
	UserID    string        `json:"user_id"`
	Status    MessageStatus `json:"status"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// ControlMessage is the envelope for inter-tier control messages sent via NATS
// on the subject nexus.{appID}.v1.ctrl.{userID}.
type ControlMessage struct {
	Ctrl    string `json:"_ctrl"` // e.g. "grp_join", "grp_leave"
	GroupID string `json:"group_id,omitempty"`
}
