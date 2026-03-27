package nats

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/infra/logger"
)

// New creates and returns a NATS connection using the given config.
// The connection is configured with automatic reconnect and logging hooks.
func New(cfg *config.NATSConfig) (*nats.Conn, error) {
	log := logger.For("infra", "nats")

	opts := []nats.Option{
		nats.Name("nexus-worker"),
		nats.MaxReconnects(-1), // reconnect indefinitely
		nats.ReconnectWait(2 * time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warnf("NATS disconnected: %v", err)
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Infof("NATS reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Warn("NATS connection closed")
		}),
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS at %s: %w", cfg.URL, err)
	}

	log.Infof("Connected to NATS at %s", nc.ConnectedUrl())
	return nc, nil
}
