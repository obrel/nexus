package infra

import (
	"runtime/debug"

	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
)

// RecoverNATS is a deferred function for NATS subscription callbacks.
// It logs the panic with a stack trace and increments the recovery counter.
func RecoverNATS(scope string) {
	if r := recover(); r != nil {
		log := logger.For("infra", "recover")
		log.Errorf("Panic in NATS callback [%s]: %v\n%s", scope, r, debug.Stack())
		metrics.NATSPanicRecoveries.Inc()
	}
}
