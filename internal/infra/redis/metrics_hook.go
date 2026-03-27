package redis

import (
	"context"
	"net"
	"time"

	"github.com/obrel/nexus/internal/infra/metrics"
	"github.com/redis/go-redis/v9"
)

type startTimeKey struct{}

// MetricsHook implements redis.Hook to record command latency and errors.
type MetricsHook struct{}

var _ redis.Hook = (*MetricsHook)(nil)

func (h *MetricsHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *MetricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		ctx = context.WithValue(ctx, startTimeKey{}, start)

		err := next(ctx, cmd)

		duration := time.Since(start).Seconds()
		metrics.RedisCommandDuration.WithLabelValues(cmd.Name()).Observe(duration)
		if err != nil && err != redis.Nil {
			metrics.RedisErrorsTotal.Inc()
		}
		return err
	}
}

func (h *MetricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()

		err := next(ctx, cmds)

		duration := time.Since(start).Seconds()
		metrics.RedisCommandDuration.WithLabelValues("pipeline").Observe(duration)
		if err != nil && err != redis.Nil {
			metrics.RedisErrorsTotal.Inc()
		}
		return err
	}
}
