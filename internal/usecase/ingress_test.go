package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/snowflake"
)

// mockPublishRepository records the last SaveTx call for assertion.
type mockPublishRepository struct {
	savedMsg    *domain.Message
	savedOutbox *domain.OutboxEntry
	returnErr   error
	callCount   int
}

func (m *mockPublishRepository) SaveTx(_ context.Context, msg *domain.Message, outbox *domain.OutboxEntry) error {
	m.callCount++
	m.savedMsg = msg
	m.savedOutbox = outbox
	return m.returnErr
}

// mockOutboxWriter records Save calls for standalone outbox entries.
type mockOutboxWriter struct {
	saved     *domain.OutboxEntry
	returnErr error
}

func (m *mockOutboxWriter) Save(_ context.Context, entry *domain.OutboxEntry) error {
	m.saved = entry
	return m.returnErr
}

// mockUserGroupRepository always returns isMember for IsMember calls.
type mockUserGroupRepository struct {
	isMember bool
	err      error
}

func (m *mockUserGroupRepository) JoinGroup(ctx context.Context, appID, userID, groupID string) error {
	return nil
}
func (m *mockUserGroupRepository) LeaveGroup(ctx context.Context, appID, userID, groupID string) error {
	return nil
}
func (m *mockUserGroupRepository) GetUserGroups(ctx context.Context, appID, userID string) ([]string, error) {
	return nil, nil
}
func (m *mockUserGroupRepository) IsMember(ctx context.Context, appID, userID, groupID string) (bool, error) {
	return m.isMember, m.err
}

// helpers

func newTestUseCase(t *testing.T, repo domain.PublishRepository) MessageUseCase {
	t.Helper()
	sf, err := snowflake.New()
	if err != nil {
		t.Fatalf("snowflake.New: %v", err)
	}
	ring := hash.New(1024)
	ugRepo := &mockUserGroupRepository{isMember: true}
	uc, err := NewMessageUseCase(repo, nil, nil, ugRepo, &mockOutboxWriter{}, sf, ring)
	if err != nil {
		t.Fatalf("NewMessageUseCase: %v", err)
	}
	return uc
}

func groupMsg(recipientID, senderID string) *domain.Message {
	return &domain.Message{
		RecipientType: domain.RecipientGroup,
		RecipientID:   recipientID,
		SenderType:    domain.SenderUser,
		SenderID:      senderID,
	}
}

// --- constructor tests ---

func TestNewMessageUseCase_NilRepo(t *testing.T) {
	sf, _ := snowflake.New()
	ring := hash.New(1024)
	ugRepo := &mockUserGroupRepository{isMember: true}
	_, err := NewMessageUseCase(nil, nil, nil, ugRepo, &mockOutboxWriter{}, sf, ring)
	if err == nil {
		t.Fatal("expected error for nil publishRepo, got nil")
	}
}

func TestNewMessageUseCase_NilUserGroupRepo(t *testing.T) {
	sf, _ := snowflake.New()
	ring := hash.New(1024)
	_, err := NewMessageUseCase(&mockPublishRepository{}, nil, nil, nil, &mockOutboxWriter{}, sf, ring)
	if err == nil {
		t.Fatal("expected error for nil userGroupRepo, got nil")
	}
}

func TestNewMessageUseCase_NilSnowflake(t *testing.T) {
	ring := hash.New(1024)
	ugRepo := &mockUserGroupRepository{isMember: true}
	_, err := NewMessageUseCase(&mockPublishRepository{}, nil, nil, ugRepo, &mockOutboxWriter{}, nil, ring)
	if err == nil {
		t.Fatal("expected error for nil snowflake, got nil")
	}
}

func TestNewMessageUseCase_NilRing(t *testing.T) {
	sf, _ := snowflake.New()
	ugRepo := &mockUserGroupRepository{isMember: true}
	_, err := NewMessageUseCase(&mockPublishRepository{}, nil, nil, ugRepo, &mockOutboxWriter{}, sf, nil)
	if err == nil {
		t.Fatal("expected error for nil ring, got nil")
	}
}

// --- Publish tests ---

func TestPublish_AssignsSnowflakeID(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if msg.ID == 0 {
		t.Error("expected non-zero Snowflake ID to be assigned")
	}
}

func TestPublish_AssignsShardID(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if msg.ShardID < 0 || msg.ShardID >= 1024 {
		t.Errorf("ShardID out of range: got %d", msg.ShardID)
	}
}

func TestPublish_ShardIDIsStable(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	recipientID := "room_stability_test"
	var firstShard int
	for i := 0; i < 5; i++ {
		msg := groupMsg(recipientID, "user_1")
		if err := uc.Send(context.Background(), "app_a", msg); err != nil {
			t.Fatalf("Publish iteration %d: %v", i, err)
		}
		if i == 0 {
			firstShard = msg.ShardID
		} else if msg.ShardID != firstShard {
			t.Errorf("ShardID not stable: iteration %d got %d, expected %d", i, msg.ShardID, firstShard)
		}
	}
}

func TestPublish_NATSSubjectFormat(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_x", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	subject := repo.savedOutbox.NATSSubject
	expected := fmt.Sprintf("nexus.app_x.v1.grp.%d.room_1", msg.ShardID)
	if subject != expected {
		t.Errorf("NATS subject: got %q, want %q", subject, expected)
	}
}

func TestPublish_NATSSubjectHasCorrectPrefix(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("any_topic", "user_1")
	if err := uc.Send(context.Background(), "tenant_42", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	subject := repo.savedOutbox.NATSSubject
	if !strings.HasPrefix(subject, "nexus.tenant_42.v1.grp.") {
		t.Errorf("NATS subject has wrong prefix: %q", subject)
	}
}

func TestPublish_PrivateMessage_NATSSubject(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := &domain.Message{
		RecipientType: domain.RecipientUser,
		RecipientID:   "bob",
		SenderType:    domain.SenderUser,
		SenderID:      "alice",
	}
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	subject := repo.savedOutbox.NATSSubject
	expected := "nexus.app_a.v1.user.bob"
	if subject != expected {
		t.Errorf("NATS subject: got %q, want %q", subject, expected)
	}
}

func TestPublish_SaveTxCalledOnce(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if repo.callCount != 1 {
		t.Errorf("SaveTx called %d times, expected 1", repo.callCount)
	}
}

func TestPublish_OutboxStatusIsPending(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if repo.savedOutbox.Status != "pending" {
		t.Errorf("outbox status: got %q, want %q", repo.savedOutbox.Status, "pending")
	}
}

func TestPublish_SetsAppID(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if msg.AppID != "app_a" {
		t.Errorf("AppID: got %q, want %q", msg.AppID, "app_a")
	}
}

func TestPublish_AppIDMismatch(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := &domain.Message{AppID: "other_app", RecipientType: domain.RecipientGroup, RecipientID: "room_1", SenderType: domain.SenderUser, SenderID: "user_1"}
	err := uc.Send(context.Background(), "app_a", msg)
	if err == nil {
		t.Fatal("expected error for app_id mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "app_id mismatch") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPublish_MissingRecipientID(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := &domain.Message{RecipientType: domain.RecipientGroup, SenderType: domain.SenderUser, SenderID: "user_1"}
	err := uc.Send(context.Background(), "app_a", msg)
	if err == nil {
		t.Fatal("expected error for missing recipient_id, got nil")
	}
	if !strings.Contains(err.Error(), "recipient_id is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPublish_MissingSenderID(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := &domain.Message{RecipientType: domain.RecipientGroup, RecipientID: "room_1"}
	err := uc.Send(context.Background(), "app_a", msg)
	if err == nil {
		t.Fatal("expected error for missing sender_id, got nil")
	}
	if !strings.Contains(err.Error(), "sender_id is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPublish_PropagatesRepoError(t *testing.T) {
	repoErr := errors.New("db unavailable")
	repo := &mockPublishRepository{returnErr: repoErr}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	err := uc.Send(context.Background(), "app_a", msg)
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
	if !errors.Is(err, repoErr) {
		t.Errorf("expected wrapped repoErr, got: %v", err)
	}
}

func BenchmarkPublish(b *testing.B) {
	repo := &mockPublishRepository{}
	sf, err := snowflake.New()
	if err != nil {
		b.Fatalf("snowflake.New: %v", err)
	}
	ring := hash.New(1024)
	ugRepo := &mockUserGroupRepository{isMember: true}
	uc, err := NewMessageUseCase(repo, nil, nil, ugRepo, &mockOutboxWriter{}, sf, ring)
	if err != nil {
		b.Fatalf("NewMessageUseCase: %v", err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		msg := groupMsg("room_bench", "user_bench")
		_ = uc.Send(ctx, "app_bench", msg)
	}
}

func TestPublish_GroupMessage_NotAMember(t *testing.T) {
	repo := &mockPublishRepository{}
	sf, _ := snowflake.New()
	ring := hash.New(1024)
	ugRepo := &mockUserGroupRepository{isMember: false}
	uc, err := NewMessageUseCase(repo, nil, nil, ugRepo, &mockOutboxWriter{}, sf, ring)
	if err != nil {
		t.Fatalf("NewMessageUseCase: %v", err)
	}

	msg := groupMsg("room_1", "user_1")
	err = uc.Send(context.Background(), "app_a", msg)
	if err == nil {
		t.Fatal("expected error for non-member sending to group, got nil")
	}
	if !errors.Is(err, domain.ErrAccessDenied) {
		t.Errorf("expected ErrAccessDenied, got: %v", err)
	}
}

func TestPublish_PrivateMessage_SkipsMembershipCheck(t *testing.T) {
	repo := &mockPublishRepository{}
	sf, _ := snowflake.New()
	ring := hash.New(1024)
	ugRepo := &mockUserGroupRepository{isMember: false} // would fail if checked
	uc, err := NewMessageUseCase(repo, nil, nil, ugRepo, &mockOutboxWriter{}, sf, ring)
	if err != nil {
		t.Fatalf("NewMessageUseCase: %v", err)
	}

	msg := &domain.Message{
		RecipientType: domain.RecipientUser,
		RecipientID:   "bob",
		SenderType:    domain.SenderUser,
		SenderID:      "alice",
	}
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("expected private message to skip membership check, got: %v", err)
	}
}

func TestPublish_OutboxDataIDMatchesMsgID(t *testing.T) {
	repo := &mockPublishRepository{}
	uc := newTestUseCase(t, repo)

	msg := groupMsg("room_1", "user_1")
	if err := uc.Send(context.Background(), "app_a", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if repo.savedOutbox.DataType != domain.OutboxDataTypeMessage {
		t.Errorf("outbox.DataType=%q, want %q", repo.savedOutbox.DataType, domain.OutboxDataTypeMessage)
	}
	if repo.savedOutbox.DataID == nil || *repo.savedOutbox.DataID != msg.ID {
		var got uint64
		if repo.savedOutbox.DataID != nil {
			got = *repo.savedOutbox.DataID
		}
		t.Errorf("outbox.DataID=%d does not match msg.ID=%d", got, msg.ID)
	}
	if repo.savedOutbox.ID != msg.ID {
		t.Errorf("outbox.ID=%d does not match msg.ID=%d", repo.savedOutbox.ID, msg.ID)
	}
}
