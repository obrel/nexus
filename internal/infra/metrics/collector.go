package metrics

import (
	"context"
	"database/sql"
	"time"
)

// StartMySQLPoolCollector periodically samples sql.DB pool stats and updates
// the corresponding Prometheus gauges. Stops when ctx is cancelled.
func StartMySQLPoolCollector(ctx context.Context, db *sql.DB, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := db.Stats()
			MySQLPoolOpenConnections.Set(float64(stats.OpenConnections))
			MySQLPoolInUse.Set(float64(stats.InUse))
			MySQLPoolIdle.Set(float64(stats.Idle))
			MySQLPoolWaitCount.Set(float64(stats.WaitCount))
			MySQLPoolWaitDuration.Set(stats.WaitDuration.Seconds())
		}
	}
}
