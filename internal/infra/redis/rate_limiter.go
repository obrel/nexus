package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter provides Redis-backed rate limiting using INCR + EXPIRE.
type RateLimiter struct {
	rdb *redis.Client
}

// NewRateLimiter creates a new RateLimiter backed by the given Redis client.
func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{rdb: rdb}
}

// Allow checks whether the given key is within its rate limit.
// It increments the counter for key and sets a TTL of window on first access.
// Returns true if the current count is within maxAttempts.
func (rl *RateLimiter) Allow(ctx context.Context, key string, maxAttempts int64, window time.Duration) (bool, error) {
	count, err := rl.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// Set expiry only on the first increment (count == 1).
	if count == 1 {
		rl.rdb.Expire(ctx, key, window)
	}

	return count <= maxAttempts, nil
}
