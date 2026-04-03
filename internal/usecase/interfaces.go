package usecase

import (
	"context"

	"github.com/obrel/nexus/internal/domain"
)

// MessageUseCase handles message lifecycle operations (send, edit, delete, ack).
type MessageUseCase interface {
	Send(ctx context.Context, appID string, msg *domain.Message) error
	Edit(ctx context.Context, appID, userID string, msgID uint64, content string) error
	Delete(ctx context.Context, appID, userID string, msgID uint64) error
	Ack(ctx context.Context, appID, userID string, msgID uint64, status domain.MessageStatus) error
}

// GroupUseCase handles group creation, update, and membership operations.
type GroupUseCase interface {
	Create(ctx context.Context, appID, userID, groupID, name, description string) error
	Get(ctx context.Context, appID, userID, groupID string) (*domain.Group, error)
	Update(ctx context.Context, appID, userID, groupID, name, description string) error
	Join(ctx context.Context, appID, userID, groupID string) error
	Leave(ctx context.Context, appID, userID, groupID string) error
}

// ProfileUseCase handles user profile operations (get, update).
type ProfileUseCase interface {
	Get(ctx context.Context, appID, userID string) (*domain.UserProfile, error)
	Update(ctx context.Context, appID, userID string, profile *domain.UserProfile) error
}

// HistoryUseCase handles message history queries.
type HistoryUseCase interface {
	GetHistory(ctx context.Context, appID, userID string, recipientType domain.RecipientType, recipientID string, sinceID uint64, limit int) ([]*domain.Message, error)
}

// EventUseCase handles transient custom event dispatch via the outbox (no messages table row).
type EventUseCase interface {
	Send(ctx context.Context, appID, senderID string, evt *domain.CustomEvent) error
}

// AuthUseCase handles user registration and JWT issuance.
type AuthUseCase interface {
	// RegisterUser creates or retrieves a user by email and returns a JWT.
	// Called by external auth services to sync users into Nexus.
	RegisterUser(ctx context.Context, appID, email, name string) (token string, userID string, err error)
}

// EgressUseCase defines the Egress tier lifecycle (kept for reference, not actively used).
type EgressUseCase interface {
	Connect(ctx context.Context, appID, userID string) error
	Disconnect(ctx context.Context, appID, userID string) error
	Subscribe(ctx context.Context, appID, userID, topic string) error
	Unsubscribe(ctx context.Context, appID, userID, topic string) error
}
