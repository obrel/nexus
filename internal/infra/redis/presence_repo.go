package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/obrel/nexus/internal/domain"
	"github.com/redis/go-redis/v9"
)

// PresenceRepository implements domain.PresenceRepository using Redis.
// Key format: {appID}:user:presence:{userID} → nodeID
type PresenceRepository struct {
	rdb *redis.Client
}

// NewPresenceRepository creates a new PresenceRepository.
func NewPresenceRepository(rdb *redis.Client) *PresenceRepository {
	return &PresenceRepository{rdb: rdb}
}

// SetUserPresence stores the user's node mapping with a TTL (seconds).
func (r *PresenceRepository) SetUserPresence(ctx context.Context, appID, userID, nodeID string, ttl int) error {
	key := presenceKey(appID, userID)
	return r.rdb.Set(ctx, key, nodeID, time.Duration(ttl)*time.Second).Err()
}

// GetUserPresence returns the user's presence or nil if not found.
func (r *PresenceRepository) GetUserPresence(ctx context.Context, appID, userID string) (*domain.UserPresence, error) {
	key := presenceKey(appID, userID)
	nodeID, err := r.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis: get presence: %w", err)
	}
	return &domain.UserPresence{
		UserID: userID,
		NodeID: nodeID,
		Status: "online",
	}, nil
}

// DeleteUserPresence removes the user's presence entry.
func (r *PresenceRepository) DeleteUserPresence(ctx context.Context, appID, userID string) error {
	key := presenceKey(appID, userID)
	return r.rdb.Del(ctx, key).Err()
}

// RefreshPresenceTTL resets the TTL without changing the value (called on heartbeat).
func (r *PresenceRepository) RefreshPresenceTTL(ctx context.Context, appID, userID string, ttl int) error {
	key := presenceKey(appID, userID)
	ok, err := r.rdb.Expire(ctx, key, time.Duration(ttl)*time.Second).Result()
	if err != nil {
		return fmt.Errorf("redis: expire presence: %w", err)
	}
	if !ok {
		// Key doesn't exist (expired between heartbeats) — recreate it.
		// Value is unknown here; caller (ConnManager) must re-set via SetUserPresence.
		return fmt.Errorf("redis: presence key not found for %s/%s", appID, userID)
	}
	return nil
}

// presenceKey returns the Redis key "{appID}:user:presence:{userID}".
func presenceKey(appID, userID string) string {
	return fmt.Sprintf("%s:user:presence:%s", appID, userID)
}
