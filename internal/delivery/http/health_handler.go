package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// HealthDeps holds dependencies for health/readiness checks.
type HealthDeps struct {
	DB  *sql.DB
	RDB *redis.Client
	NC  *nats.Conn
}

// HealthHandler returns 200 if the process is alive. No dependency checks.
func HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
	}
}

// ReadyHandler returns 200 only if all critical dependencies (MySQL, Redis, NATS) are reachable.
func ReadyHandler(deps *HealthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{}
		healthy := true

		// MySQL
		if deps.DB != nil {
			if err := deps.DB.PingContext(ctx); err != nil {
				checks["mysql"] = err.Error()
				healthy = false
			} else {
				checks["mysql"] = "ok"
			}
		}

		// Redis
		if deps.RDB != nil {
			if err := deps.RDB.Ping(ctx).Err(); err != nil {
				checks["redis"] = err.Error()
				healthy = false
			} else {
				checks["redis"] = "ok"
			}
		}

		// NATS
		if deps.NC != nil {
			if deps.NC.IsConnected() {
				checks["nats"] = "ok"
			} else {
				checks["nats"] = "disconnected"
				healthy = false
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if healthy {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"status": map[bool]string{true: "ready", false: "not_ready"}[healthy],
			"checks": checks,
		})
	}
}
