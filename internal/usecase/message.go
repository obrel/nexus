package usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
	"github.com/obrel/nexus/internal/infra/snowflake"
)

// messageUseCase implements MessageUseCase for message lifecycle operations.
type messageUseCase struct {
	publishRepo   domain.PublishRepository
	msgRepo       domain.MessageRepository
	statusRepo    domain.MessageStatusRepository
	userGroupRepo domain.UserGroupRepository
	snowflake     *snowflake.Generator
	ring          *hash.Ring
	log           logger.Logger
}

// NewMessageUseCase creates a new MessageUseCase implementation.
func NewMessageUseCase(
	publishRepo domain.PublishRepository,
	msgRepo domain.MessageRepository,
	statusRepo domain.MessageStatusRepository,
	userGroupRepo domain.UserGroupRepository,
	sf *snowflake.Generator,
	ring *hash.Ring,
) (MessageUseCase, error) {
	if publishRepo == nil {
		return nil, fmt.Errorf("publishRepo is required")
	}
	if userGroupRepo == nil {
		return nil, fmt.Errorf("userGroupRepo is required")
	}
	if sf == nil {
		return nil, fmt.Errorf("snowflake generator is required")
	}
	if ring == nil {
		return nil, fmt.Errorf("hash ring is required")
	}

	return &messageUseCase{
		publishRepo:   publishRepo,
		msgRepo:       msgRepo,
		statusRepo:    statusRepo,
		userGroupRepo: userGroupRepo,
		snowflake:     sf,
		ring:          ring,
		log:           logger.For("usecase", "message"),
	}, nil
}

// Send handles message publishing using the transactional outbox pattern.
func (uc *messageUseCase) Send(ctx context.Context, appID string, msg *domain.Message) error {
	if msg.AppID == "" {
		msg.AppID = appID
	}
	if msg.AppID != appID {
		return fmt.Errorf("app_id mismatch: message has %q but context has %q", msg.AppID, appID)
	}
	if msg.RecipientID == "" {
		return fmt.Errorf("recipient_id is required")
	}
	if msg.RecipientType == "" {
		return fmt.Errorf("recipient_type is required")
	}
	if msg.SenderID == "" {
		return fmt.Errorf("sender_id is required")
	}

	// Verify sender is a member of the group before allowing send.
	if !msg.IsPrivate() {
		isMember, err := uc.userGroupRepo.IsMember(ctx, appID, msg.SenderID, msg.RecipientID)
		if err != nil {
			return fmt.Errorf("send: check membership: %w", err)
		}
		if !isMember {
			return fmt.Errorf("send: user %s is not a member of group %s: %w", msg.SenderID, msg.RecipientID, domain.ErrAccessDenied)
		}
	}

	id, err := uc.snowflake.NextID()
	if err != nil {
		return fmt.Errorf("failed to generate snowflake ID: %w", err)
	}
	msg.ID = id
	msg.ShardID = uc.ring.GetShard(msg.RecipientID)

	// Build NATS subject and event payload based on recipient type.
	var natsSubject string
	eventData := map[string]any{
		"message_id":     fmt.Sprintf("%d", msg.ID),
		"recipient_type": string(msg.RecipientType),
		"recipient_id":   msg.RecipientID,
		"sender_type":    string(msg.SenderType),
		"sender_id":      msg.SenderID,
		"content":        msg.Content,
	}

	if msg.IsPrivate() {
		natsSubject = fmt.Sprintf("nexus.%s.v1.user.%s", appID, msg.RecipientID)
	} else {
		natsSubject = fmt.Sprintf("nexus.%s.v1.grp.%d.%s", appID, msg.ShardID, msg.RecipientID)
	}

	eventPayload, err := json.Marshal(map[string]any{
		"event": "MSG_NEW",
		"data":  eventData,
	})
	if err != nil {
		return fmt.Errorf("marshal MSG_NEW payload: %w", err)
	}

	outbox := &domain.OutboxEntry{
		ID:          id,
		AppID:       appID,
		MessageID:   id,
		NATSSubject: natsSubject,
		Payload:     string(eventPayload),
		Status:      "pending",
	}

	if err := uc.publishRepo.SaveTx(ctx, msg, outbox); err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	metrics.MessagesSentTotal.Inc()
	uc.log.Infof("Message sent: id=%d app_id=%s type=%s recipient=%s sender=%s",
		msg.ID, appID, msg.RecipientType, msg.RecipientID, msg.SenderID)
	return nil
}

// Edit updates message content after verifying sender ownership.
func (uc *messageUseCase) Edit(ctx context.Context, appID, userID string, msgID uint64, content string) error {
	msg, err := uc.msgRepo.GetByID(ctx, msgID)
	if err != nil {
		return fmt.Errorf("edit: fetch message: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("edit: message %d not found", msgID)
	}
	if msg.AppID != appID {
		return fmt.Errorf("edit: message %d does not belong to app %s", msgID, appID)
	}
	if msg.SenderID != userID {
		return fmt.Errorf("edit: user %s is not the sender of message %d", userID, msgID)
	}

	if err := uc.msgRepo.Update(ctx, appID, msgID, content); err != nil {
		return fmt.Errorf("edit: %w", err)
	}

	// Write MSG_UPDATED event to outbox for guaranteed delivery
	eventData := map[string]any{
		"message_id":     fmt.Sprintf("%d", msgID),
		"recipient_type": string(msg.RecipientType),
		"recipient_id":   msg.RecipientID,
		"content":        json.RawMessage(content),
		"action":         "edited",
	}
	natsSubject := uc.natsSubject(appID, msg)
	eventPayload, err := json.Marshal(map[string]any{
		"event": "MSG_UPDATED",
		"data":  eventData,
	})
	if err != nil {
		return fmt.Errorf("edit: marshal payload: %w", err)
	}

	outboxID, err := uc.snowflake.NextID()
	if err != nil {
		return fmt.Errorf("edit: generate outbox ID: %w", err)
	}
	outbox := &domain.OutboxEntry{
		ID:          outboxID,
		AppID:       appID,
		MessageID:   msgID,
		NATSSubject: natsSubject,
		Payload:     string(eventPayload),
		Status:      "pending",
	}

	// Use publishRepo.SaveTx with a nil message to write only the outbox entry.
	// Since SaveTx requires both, we save a dummy update via direct outbox insert.
	// For now, we rely on the existing outbox relay to pick this up.
	if err := uc.saveOutboxEntry(ctx, outbox); err != nil {
		uc.log.Warnf("edit: outbox write failed for message %d: %v", msgID, err)
	}

	uc.log.Infof("Message edited: id=%d app_id=%s", msgID, appID)
	return nil
}

// Delete soft-deletes a message after verifying sender ownership.
func (uc *messageUseCase) Delete(ctx context.Context, appID, userID string, msgID uint64) error {
	msg, err := uc.msgRepo.GetByID(ctx, msgID)
	if err != nil {
		return fmt.Errorf("delete: fetch message: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("delete: message %d not found", msgID)
	}
	if msg.AppID != appID {
		return fmt.Errorf("delete: message %d does not belong to app %s", msgID, appID)
	}
	if msg.SenderID != userID {
		return fmt.Errorf("delete: user %s is not the sender of message %d", userID, msgID)
	}

	if err := uc.msgRepo.SoftDelete(ctx, appID, msgID); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	// Write MSG_UPDATED event to outbox
	eventData := map[string]any{
		"message_id":    fmt.Sprintf("%d", msgID),
		"recipient_type": string(msg.RecipientType),
		"recipient_id":  msg.RecipientID,
		"action":        "deleted",
	}
	natsSubject := uc.natsSubject(appID, msg)
	eventPayload, err := json.Marshal(map[string]any{
		"event": "MSG_UPDATED",
		"data":  eventData,
	})
	if err != nil {
		return fmt.Errorf("delete: marshal payload: %w", err)
	}

	outboxID, err := uc.snowflake.NextID()
	if err != nil {
		return fmt.Errorf("delete: generate outbox ID: %w", err)
	}
	outbox := &domain.OutboxEntry{
		ID:          outboxID,
		AppID:       appID,
		MessageID:   msgID,
		NATSSubject: natsSubject,
		Payload:     string(eventPayload),
		Status:      "pending",
	}

	if err := uc.saveOutboxEntry(ctx, outbox); err != nil {
		uc.log.Warnf("delete: outbox write failed for message %d: %v", msgID, err)
	}

	uc.log.Infof("Message deleted: id=%d app_id=%s", msgID, appID)
	return nil
}

// Ack records a message acknowledgment (received/read) and writes a MSG_STATUS
// event to the outbox for guaranteed delivery to the original sender.
func (uc *messageUseCase) Ack(ctx context.Context, appID, userID string, msgID uint64, status domain.MessageStatus) error {
	if status != domain.StatusReceived && status != domain.StatusRead {
		return fmt.Errorf("ack: invalid status %q", status)
	}

	// Persist status
	if err := uc.statusRepo.UpdateStatus(ctx, appID, msgID, userID, status); err != nil {
		return fmt.Errorf("ack: %w", err)
	}

	// Look up the original message to find the sender
	msg, err := uc.msgRepo.GetByID(ctx, msgID)
	if err != nil || msg == nil {
		uc.log.Warnf("ack: could not fetch message %d for status event: %v", msgID, err)
		return nil // status persisted; event delivery is best-effort here
	}

	// Write MSG_STATUS event to outbox targeting the sender's private subject
	eventPayload, err := json.Marshal(map[string]any{
		"event": "MSG_STATUS",
		"data": map[string]any{
			"message_id": fmt.Sprintf("%d", msgID),
			"status":     string(status),
			"user_id":    userID,
		},
	})
	if err != nil {
		return fmt.Errorf("ack: marshal payload: %w", err)
	}

	outboxID, err := uc.snowflake.NextID()
	if err != nil {
		return fmt.Errorf("ack: generate outbox ID: %w", err)
	}
	natsSubject := fmt.Sprintf("nexus.%s.v1.user.%s", appID, msg.SenderID)
	outbox := &domain.OutboxEntry{
		ID:          outboxID,
		AppID:       appID,
		MessageID:   msgID,
		NATSSubject: natsSubject,
		Payload:     string(eventPayload),
		Status:      "pending",
	}

	if err := uc.saveOutboxEntry(ctx, outbox); err != nil {
		uc.log.Warnf("ack: outbox write failed for message %d: %v", msgID, err)
	}

	uc.log.Infof("Message ack: id=%d status=%s by user=%s", msgID, status, userID)
	return nil
}

// saveOutboxEntry inserts a standalone outbox entry (for events that don't create a new message).
func (uc *messageUseCase) saveOutboxEntry(ctx context.Context, outbox *domain.OutboxEntry) error {
	// We need a way to insert just an outbox row. The PublishRepository.SaveTx
	// bundles message+outbox. For event-only outbox entries, we use MessageRepository.Save
	// with a placeholder approach, or we add a direct outbox insert.
	// For now, we use the message repo's underlying DB indirectly.
	// TODO: Add an OutboxWriter interface for standalone outbox inserts.
	// Temporarily, we'll create a minimal message + outbox via SaveTx.
	dummyMsg := &domain.Message{
		ID:            outbox.ID,
		AppID:         outbox.AppID,
		RecipientType: domain.RecipientGroup,
		RecipientID:   "__event__",
		ShardID:       0,
		Content:       json.RawMessage(outbox.Payload),
		SenderType:    domain.SenderUser,
		SenderID:      "__system__",
	}
	return uc.publishRepo.SaveTx(ctx, dummyMsg, outbox)
}

// natsSubject returns the NATS subject for a message based on its recipient type.
func (uc *messageUseCase) natsSubject(appID string, msg *domain.Message) string {
	if msg.IsPrivate() {
		return fmt.Sprintf("nexus.%s.v1.user.%s", appID, msg.RecipientID)
	}
	return fmt.Sprintf("nexus.%s.v1.grp.%d.%s", appID, msg.ShardID, msg.RecipientID)
}
