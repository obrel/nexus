package usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/internal/domain"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/logger"
)

// groupUseCase implements GroupUseCase for group creation and membership operations.
type groupUseCase struct {
	groupRepo     domain.GroupRepository
	userGroupRepo domain.UserGroupRepository
	ring          *hash.Ring
	nc            *nats.Conn
	log           logger.Logger
}

// NewGroupUseCase creates a new GroupUseCase implementation.
func NewGroupUseCase(
	groupRepo domain.GroupRepository,
	userGroupRepo domain.UserGroupRepository,
	ring *hash.Ring,
	nc *nats.Conn,
) (GroupUseCase, error) {
	if groupRepo == nil {
		return nil, fmt.Errorf("groupRepo is required")
	}
	if userGroupRepo == nil {
		return nil, fmt.Errorf("userGroupRepo is required")
	}
	if ring == nil {
		return nil, fmt.Errorf("hash ring is required")
	}
	if nc == nil {
		return nil, fmt.Errorf("nats connection is required")
	}
	return &groupUseCase{
		groupRepo:     groupRepo,
		userGroupRepo: userGroupRepo,
		ring:          ring,
		nc:            nc,
		log:           logger.For("usecase", "group"),
	}, nil
}

// Create registers a new group with its computed shard ID, name, description, and creator.
// Returns an error if the group already exists.
func (uc *groupUseCase) Create(ctx context.Context, appID, userID, groupID, name, description string) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}

	// Check if group already exists
	existing, err := uc.groupRepo.GetGroup(ctx, appID, groupID)
	if err != nil {
		return fmt.Errorf("create group: %w", err)
	}
	if existing != nil {
		return fmt.Errorf("create group: group %s: %w", groupID, domain.ErrAlreadyExists)
	}

	shard := uc.ring.GetShard(groupID)
	group := &domain.Group{
		ID:          groupID,
		AppID:       appID,
		Name:        name,
		Description: description,
		ShardID:     shard,
		CreatedBy:   userID,
	}

	if err := uc.groupRepo.Create(ctx, group); err != nil {
		return fmt.Errorf("create group: %w", err)
	}

	uc.log.Infof("Group created: %s (app=%s, shard=%d, by=%s)", groupID, appID, shard, userID)
	return nil
}

// Get returns the details of an existing group. Only group members can view group details.
func (uc *groupUseCase) Get(ctx context.Context, appID, userID, groupID string) (*domain.Group, error) {
	if groupID == "" {
		return nil, fmt.Errorf("group_id is required")
	}

	group, err := uc.groupRepo.GetGroup(ctx, appID, groupID)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	if group == nil {
		return nil, fmt.Errorf("get group: group %s: %w", groupID, domain.ErrNotFound)
	}

	// Verify user is a member of the group
	isMember, err := uc.userGroupRepo.IsMember(ctx, appID, userID, groupID)
	if err != nil {
		return nil, fmt.Errorf("get group: check membership: %w", err)
	}
	if !isMember {
		return nil, fmt.Errorf("get group: user %s is not a member of group %s: %w", userID, groupID, domain.ErrAccessDenied)
	}

	return group, nil
}

// Update modifies the name and description of an existing group.
// Only group members are allowed to update.
func (uc *groupUseCase) Update(ctx context.Context, appID, userID, groupID, name, description string) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}

	// Verify group exists
	group, err := uc.groupRepo.GetGroup(ctx, appID, groupID)
	if err != nil {
		return fmt.Errorf("update group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("update group: group %s: %w", groupID, domain.ErrNotFound)
	}

	// Verify user is a member of the group
	isMember, err := uc.userGroupRepo.IsMember(ctx, appID, userID, groupID)
	if err != nil {
		return fmt.Errorf("update group: check membership: %w", err)
	}
	if !isMember {
		return fmt.Errorf("update group: user %s is not a member of group %s: %w", userID, groupID, domain.ErrAccessDenied)
	}

	if err := uc.groupRepo.Update(ctx, appID, groupID, name, description); err != nil {
		return fmt.Errorf("update group: %w", err)
	}

	uc.log.Infof("Group updated: %s (app=%s, by=%s)", groupID, appID, userID)
	return nil
}

// Join adds a user to a group, notifies the Egress node via NATS control channel,
// and broadcasts a USR_JOINED event to group members.
// Returns an error if the group does not exist.
func (uc *groupUseCase) Join(ctx context.Context, appID, userID, groupID string) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}

	// Verify group exists
	group, err := uc.groupRepo.GetGroup(ctx, appID, groupID)
	if err != nil {
		return fmt.Errorf("join group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("join group: group %s: %w", groupID, domain.ErrNotFound)
	}

	// 1. Persist membership
	if err := uc.userGroupRepo.JoinGroup(ctx, appID, userID, groupID); err != nil {
		return fmt.Errorf("join group: %w", err)
	}

	// 2. Publish control message to Egress node via NATS
	ctrl := domain.ControlMessage{Ctrl: "grp_join", GroupID: groupID}
	ctrlData, err := json.Marshal(ctrl)
	if err != nil {
		return fmt.Errorf("join group: marshal control: %w", err)
	}
	ctrlSubject := fmt.Sprintf("nexus.%s.v1.ctrl.%s", appID, userID)
	if err := uc.nc.Publish(ctrlSubject, ctrlData); err != nil {
		uc.log.Warnf("join group: control publish failed for %s/%s: %v", appID, userID, err)
	}

	// 3. Broadcast USR_JOINED event to group members
	event, err := json.Marshal(map[string]any{
		"event": "USR_JOINED",
		"data": map[string]string{
			"group_id": groupID,
			"user_id":  userID,
		},
	})
	if err != nil {
		return fmt.Errorf("join group: marshal event: %w", err)
	}
	shard := uc.ring.GetShard(groupID)
	grpSubject := fmt.Sprintf("nexus.%s.v1.grp.%d.%s", appID, shard, groupID)
	if err := uc.nc.Publish(grpSubject, event); err != nil {
		uc.log.Warnf("join group: event publish failed: %v", err)
	}

	uc.log.Infof("User %s joined group %s (app=%s)", userID, groupID, appID)
	return nil
}

// Leave removes a user from a group, notifies the Egress node, and broadcasts USR_LEFT.
func (uc *groupUseCase) Leave(ctx context.Context, appID, userID, groupID string) error {
	if groupID == "" {
		return fmt.Errorf("group_id is required")
	}

	// 1. Remove membership
	if err := uc.userGroupRepo.LeaveGroup(ctx, appID, userID, groupID); err != nil {
		return fmt.Errorf("leave group: %w", err)
	}

	// 2. Publish control message
	ctrl := domain.ControlMessage{Ctrl: "grp_leave", GroupID: groupID}
	ctrlData, err := json.Marshal(ctrl)
	if err != nil {
		return fmt.Errorf("leave group: marshal control: %w", err)
	}
	ctrlSubject := fmt.Sprintf("nexus.%s.v1.ctrl.%s", appID, userID)
	if err := uc.nc.Publish(ctrlSubject, ctrlData); err != nil {
		uc.log.Warnf("leave group: control publish failed: %v", err)
	}

	// 3. Broadcast USR_LEFT event
	event, err := json.Marshal(map[string]any{
		"event": "USR_LEFT",
		"data": map[string]string{
			"group_id": groupID,
			"user_id":  userID,
		},
	})
	if err != nil {
		return fmt.Errorf("leave group: marshal event: %w", err)
	}
	shard := uc.ring.GetShard(groupID)
	grpSubject := fmt.Sprintf("nexus.%s.v1.grp.%d.%s", appID, shard, groupID)
	if err := uc.nc.Publish(grpSubject, event); err != nil {
		uc.log.Warnf("leave group: event publish failed: %v", err)
	}

	uc.log.Infof("User %s left group %s (app=%s)", userID, groupID, appID)
	return nil
}
