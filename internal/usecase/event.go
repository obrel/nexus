package usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/snowflake"
)

// eventUseCase implements EventUseCase for transient custom event dispatch.
type eventUseCase struct {
	outboxWriter  domain.OutboxWriter
	userGroupRepo domain.UserGroupRepository
	snowflake     *snowflake.Generator
	ring          *hash.Ring
	log           logger.Logger
}

// NewEventUseCase creates a new EventUseCase implementation.
func NewEventUseCase(
	outboxWriter domain.OutboxWriter,
	userGroupRepo domain.UserGroupRepository,
	sf *snowflake.Generator,
	ring *hash.Ring,
) (EventUseCase, error) {
	if outboxWriter == nil {
		return nil, fmt.Errorf("outboxWriter is required")
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

	return &eventUseCase{
		outboxWriter:  outboxWriter,
		userGroupRepo: userGroupRepo,
		snowflake:     sf,
		ring:          ring,
		log:           logger.For("usecase", "event"),
	}, nil
}

// Send dispatches a custom event via the outbox for at-least-once delivery.
// No message row is created; only an outbox entry is written.
func (uc *eventUseCase) Send(ctx context.Context, appID, senderID string, evt *domain.CustomEvent) error {
	if evt.EventType == "" {
		return fmt.Errorf("event type is required")
	}
	if evt.RecipientID == "" {
		return fmt.Errorf("recipient_id is required")
	}

	// Group events require the sender to be a member.
	if evt.RecipientType == domain.RecipientGroup {
		isMember, err := uc.userGroupRepo.IsMember(ctx, appID, senderID, evt.RecipientID)
		if err != nil {
			return fmt.Errorf("send event: check membership: %w", err)
		}
		if !isMember {
			return fmt.Errorf("send event: user %s is not a member of group %s: %w",
				senderID, evt.RecipientID, domain.ErrAccessDenied)
		}
	}

	// Compute NATS subject — same topology as messages.
	var natsSubject string
	if evt.RecipientType == domain.RecipientUser {
		natsSubject = fmt.Sprintf("nexus.%s.v1.user.%s", appID, evt.RecipientID)
	} else {
		shardID := uc.ring.GetShard(evt.RecipientID)
		natsSubject = fmt.Sprintf("nexus.%s.v1.grp.%d.%s", appID, shardID, evt.RecipientID)
	}

	// Build NATS payload: {"event": <type>, "data": <value or {}>}
	value := evt.Value
	if len(value) == 0 {
		value = json.RawMessage("{}")
	}
	payload, err := json.Marshal(map[string]any{
		"event": evt.EventType,
		"data":  value,
	})
	if err != nil {
		return fmt.Errorf("send event: marshal payload: %w", err)
	}

	id, err := uc.snowflake.NextID()
	if err != nil {
		return fmt.Errorf("send event: generate id: %w", err)
	}

	entry := &domain.OutboxEntry{
		ID:          id,
		AppID:       appID,
		DataType:    domain.OutboxDataTypeEvent,
		DataID:      nil,
		NATSSubject: natsSubject,
		Payload:     string(payload),
		Status:      "pending",
	}
	if err := uc.outboxWriter.Save(ctx, entry); err != nil {
		return fmt.Errorf("send event: save outbox: %w", err)
	}

	uc.log.Infof("Event sent: id=%d app_id=%s type=%s recipient_type=%s recipient=%s sender=%s",
		id, appID, evt.EventType, evt.RecipientType, evt.RecipientID, senderID)
	return nil
}
