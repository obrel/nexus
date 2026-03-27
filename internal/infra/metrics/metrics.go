package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Nexus application metrics.
var (
	// --- HTTP ---

	// HTTP request duration histogram, partitioned by method, path, and status code.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "nexus",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"method", "path", "status"})

	// --- WebSocket ---

	// WebSocket connections currently active.
	WSConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "ws",
		Name:      "connections_active",
		Help:      "Number of active WebSocket connections.",
	})

	// --- Messages ---

	// Total messages sent through the ingress pipeline.
	MessagesSentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "messages",
		Name:      "sent_total",
		Help:      "Total number of messages sent via ingress.",
	})

	// Total messages delivered to WebSocket clients.
	MessagesDeliveredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "messages",
		Name:      "delivered_total",
		Help:      "Total number of messages delivered to WebSocket clients.",
	})

	// --- Outbox ---

	// Outbox entries relayed per batch.
	OutboxRelayedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "outbox",
		Name:      "relayed_total",
		Help:      "Total number of outbox entries successfully relayed to NATS.",
	})

	// Outbox relay errors.
	OutboxRelayErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "outbox",
		Name:      "relay_errors_total",
		Help:      "Total number of outbox relay batch errors.",
	})

	// Outbox pending gauge — set on each poll cycle.
	OutboxPendingGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "outbox",
		Name:      "pending",
		Help:      "Number of outbox entries processed in the last batch (proxy for lag).",
	})

	// --- NATS ---

	// NATS callbacks recovered from panic.
	NATSPanicRecoveries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "nats",
		Name:      "panic_recoveries_total",
		Help:      "Total number of panics recovered in NATS callbacks.",
	})

	// NATS publish errors.
	NATSPublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "nats",
		Name:      "publish_errors_total",
		Help:      "Total number of NATS publish errors.",
	})

	// --- MySQL ---

	// MySQL connection pool open connections (updated by collector).
	MySQLPoolOpenConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "mysql",
		Name:      "pool_open_connections",
		Help:      "Number of open MySQL connections (including idle).",
	})

	// MySQL connection pool in-use connections.
	MySQLPoolInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "mysql",
		Name:      "pool_in_use",
		Help:      "Number of MySQL connections currently in use.",
	})

	// MySQL connection pool idle connections.
	MySQLPoolIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "mysql",
		Name:      "pool_idle",
		Help:      "Number of idle MySQL connections.",
	})

	// MySQL wait count — total number of times a connection was waited for.
	MySQLPoolWaitCount = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "mysql",
		Name:      "pool_wait_count_total",
		Help:      "Total number of times a MySQL connection was waited for.",
	})

	// MySQL wait duration — total time spent waiting for connections.
	MySQLPoolWaitDuration = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "nexus",
		Subsystem: "mysql",
		Name:      "pool_wait_duration_seconds",
		Help:      "Total time spent waiting for MySQL connections.",
	})

	// --- Redis ---

	// Redis command duration histogram.
	RedisCommandDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "nexus",
		Subsystem: "redis",
		Name:      "command_duration_seconds",
		Help:      "Redis command latency in seconds.",
		Buckets:   []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5},
	}, []string{"command"})

	// Redis errors total.
	RedisErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "redis",
		Name:      "errors_total",
		Help:      "Total number of Redis command errors.",
	})

	// --- Shard throughput ---

	// Per-shard message throughput counter.
	ShardMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "nexus",
		Subsystem: "shard",
		Name:      "messages_total",
		Help:      "Total messages relayed per shard.",
	}, []string{"shard"})
)
