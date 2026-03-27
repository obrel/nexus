package usecase

import (
	"context"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/logger"
)

// defaultHistoryLimit is the number of messages returned when no limit is specified.
const defaultHistoryLimit = 25

// maxHistoryLimit caps the maximum number of messages per history query.
const maxHistoryLimit = 100

// historyUseCase implements HistoryUseCase for message history queries.
type historyUseCase struct {
	msgRepo       domain.MessageRepository
	userGroupRepo domain.UserGroupRepository
	log           logger.Logger
}

// NewHistoryUseCase creates a new HistoryUseCase implementation.
func NewHistoryUseCase(msgRepo domain.MessageRepository, userGroupRepo domain.UserGroupRepository) (HistoryUseCase, error) {
	if msgRepo == nil {
		return nil, fmt.Errorf("msgRepo is required")
	}
	if userGroupRepo == nil {
		return nil, fmt.Errorf("userGroupRepo is required")
	}
	return &historyUseCase{
		msgRepo:       msgRepo,
		userGroupRepo: userGroupRepo,
		log:           logger.For("usecase", "history"),
	}, nil
}

// GetHistory returns messages based on recipient type.
// For groups: verifies membership, then queries by group recipient_id.
// For users: returns the private conversation between userID and recipientID (bidirectional).
func (uc *historyUseCase) GetHistory(ctx context.Context, appID, userID string, recipientType domain.RecipientType, recipientID string, sinceID uint64, limit int) ([]*domain.Message, error) {
	if recipientID == "" {
		return nil, fmt.Errorf("recipient_id is required")
	}
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	switch recipientType {
	case domain.RecipientGroup:
		isMember, err := uc.userGroupRepo.IsMember(ctx, appID, userID, recipientID)
		if err != nil {
			return nil, fmt.Errorf("check membership: %w", err)
		}
		if !isMember {
			return nil, fmt.Errorf("not a group member: %w", domain.ErrAccessDenied)
		}
		return uc.msgRepo.GetGroupMessages(ctx, appID, recipientID, sinceID, limit)

	case domain.RecipientUser:
		return uc.msgRepo.GetPrivateMessages(ctx, appID, userID, recipientID, sinceID, limit)

	default:
		return nil, fmt.Errorf("invalid recipient_type: %q", recipientType)
	}
}
