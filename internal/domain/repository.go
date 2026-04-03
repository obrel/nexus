package domain

import "context"

// MessageRepository defines methods for reading and writing messages in MySQL.
type MessageRepository interface {
	Save(ctx context.Context, msg *Message) error
	GetByID(ctx context.Context, id uint64) (*Message, error)
	GetGroupMessages(ctx context.Context, appID, groupID string, sinceID uint64, limit int) ([]*Message, error)
	GetPrivateMessages(ctx context.Context, appID, userA, userB string, sinceID uint64, limit int) ([]*Message, error)
	Update(ctx context.Context, appID string, msgID uint64, content string) error
	SoftDelete(ctx context.Context, appID string, msgID uint64) error
}

// PublishRepository supports atomicity for Outbox pattern message publishing.
type PublishRepository interface {
	SaveTx(ctx context.Context, msg *Message, outbox *OutboxEntry) error
}

// OutboxWriter inserts a standalone outbox entry without an associated message row.
// Used for transient custom events that must not be saved to the messages table.
type OutboxWriter interface {
	Save(ctx context.Context, entry *OutboxEntry) error
}

// OutboxRepository handles relay-worker access to the outbox table.
// ProcessBatch fetches up to `limit` pending rows with SELECT FOR UPDATE SKIP LOCKED,
// calls fn with the entire batch, then marks all rows published (fn returns nil)
// or all rows failed (fn returns error). Batch semantics allow a single NATS flush
// after all Publish calls instead of one flush per message.
// Returns the number of rows processed and any fatal repository error.
type OutboxRepository interface {
	ProcessBatch(ctx context.Context, limit int, fn func([]*OutboxEntry) error) (int, error)
}

// PresenceRepository controls user online/offline status, primarily in Redis.
type PresenceRepository interface {
	SetUserPresence(ctx context.Context, appID, userID, nodeID string, ttl int) error
	GetUserPresence(ctx context.Context, appID, userID string) (*UserPresence, error)
	DeleteUserPresence(ctx context.Context, appID, userID string) error
	// RefreshPresenceTTL resets the TTL of an existing presence key without
	// changing its value. Called on every heartbeat to keep the key alive.
	RefreshPresenceTTL(ctx context.Context, appID, userID string, ttl int) error
}

// GroupRepository handles group creation, lookup, update, and membership queries.
type GroupRepository interface {
	Create(ctx context.Context, group *Group) error
	GetGroup(ctx context.Context, appID, groupID string) (*Group, error)
	Update(ctx context.Context, appID, groupID, name, description string) error
	GetGroupMembers(ctx context.Context, appID, groupID string) ([]string, error)
}

// UserGroupRepository manages user-group membership (user_groups table).
type UserGroupRepository interface {
	JoinGroup(ctx context.Context, appID, userID, groupID string) error
	LeaveGroup(ctx context.Context, appID, userID, groupID string) error
	GetUserGroups(ctx context.Context, appID, userID string) ([]string, error)
	IsMember(ctx context.Context, appID, userID, groupID string) (bool, error)
}

// MessageStatusRepository tracks per-user message delivery state (message_status table).
type MessageStatusRepository interface {
	UpdateStatus(ctx context.Context, appID string, messageID uint64, userID string, status MessageStatus) error
	GetStatus(ctx context.Context, appID string, messageID uint64, userID string) (*MessageStatusEntry, error)
}

// UserRepository manages user profiles.
type UserRepository interface {
	GetByID(ctx context.Context, appID, userID string) (*UserProfile, error)
	GetByEmail(ctx context.Context, appID, email string) (*UserProfile, error)
	Create(ctx context.Context, profile *UserProfile) error
	Update(ctx context.Context, appID, userID string, profile *UserProfile) error
}
