package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/obrel/nexus/config"
	"github.com/redis/go-redis/v9"
)

// New creates and validates a Redis client from configuration.
func New(cfg *config.RedisConfig) (*redis.Client, error) {
	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	// Configure TLS if enabled
	if cfg.TLS {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: cfg.Insecure,
		}
	}

	rdb := redis.NewClient(opts)

	rdb.AddHook(&MetricsHook{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: ping failed: %w", err)
	}

	return rdb, nil
}
