package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
	"github.com/obrel/nexus/internal/infra/mysql"
	natsclient "github.com/obrel/nexus/internal/infra/nats"
	"github.com/obrel/nexus/internal/usecase"
	"github.com/spf13/cobra"
)

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Start the Nexus background workers (outbox relay)",
	Run: func(cmd *cobra.Command, args []string) {
		if cfg == nil {
			logger.Fatal("Configuration not initialized - ensure config file exists")
			return
		}

		log := logger.For("infra", "worker")

		// MySQL
		db, err := mysql.New(&cfg.MySQL)
		if err != nil {
			log.Fatalf("Failed to initialize MySQL: %v", err)
			return
		}
		defer db.Close()

		// NATS
		nc, err := natsclient.New(&cfg.NATS)
		if err != nil {
			log.Fatalf("Failed to connect to NATS: %v", err)
			return
		}
		defer nc.Drain() //nolint:errcheck

		// Repositories
		outboxRepo := mysql.NewOutboxRepository(db)

		// Worker
		worker, err := usecase.NewOutboxRelayWorker(outboxRepo, nc, cfg.Worker.BatchSize, cfg.Worker.PollInterval)
		if err != nil {
			log.Fatalf("Failed to create OutboxRelayWorker: %v", err)
			return
		}

		// Graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())

		// Start MySQL pool metrics collector
		go metrics.StartMySQLPoolCollector(ctx, db, 15*time.Second)
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-quit
			log.Info("Shutting down outbox relay worker...")
			cancel()
		}()

		if err := worker.Run(ctx); err != nil && err != context.Canceled {
			log.Errorf("Worker exited with error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(workerCmd)
}
