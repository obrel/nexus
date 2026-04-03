package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpsvr "github.com/obrel/nexus/internal/delivery/http"
	"github.com/obrel/nexus/internal/infra/hash"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/obrel/nexus/internal/infra/metrics"
	"github.com/obrel/nexus/internal/infra/mysql"
	natsclient "github.com/obrel/nexus/internal/infra/nats"
	redisinfra "github.com/obrel/nexus/internal/infra/redis"
	"github.com/obrel/nexus/internal/infra/snowflake"
	"github.com/obrel/nexus/internal/usecase"
	"github.com/spf13/cobra"
)

var httpCmd = &cobra.Command{
	Use:   "http",
	Short: "Start the Nexus Ingress HTTP server",
	Run: func(cmd *cobra.Command, args []string) {
		if cfg == nil {
			logger.Fatal("Configuration not initialized - ensure config file exists")
			return
		}

		log := logger.For("delivery", "http")

		// Initialize MySQL client
		db, err := mysql.New(&cfg.MySQL)
		if err != nil {
			log.Fatalf("Failed to initialize MySQL: %v", err)
			return
		}
		defer db.Close()

		// Start MySQL pool metrics collector
		metricsCtx, metricsCancel := context.WithCancel(context.Background())
		defer metricsCancel()
		go metrics.StartMySQLPoolCollector(metricsCtx, db, 15*time.Second)

		// Initialize NATS connection (needed for group control messages)
		nc, err := natsclient.New(&cfg.NATS)
		if err != nil {
			log.Fatalf("Failed to connect to NATS: %v", err)
			return
		}
		defer nc.Close()

		// Initialize Redis client
		rdb, err := redisinfra.New(&cfg.Redis)
		if err != nil {
			log.Fatalf("Failed to initialize Redis: %v", err)
			return
		}
		defer rdb.Close()

		// Initialize infrastructure components
		snowflakeGen, err := snowflake.New()
		if err != nil {
			log.Fatalf("Failed to initialize Snowflake: %v", err)
			return
		}

		hashRing := hash.New(1024) // 1024 shards

		// Initialize repositories
		publishRepo := mysql.NewPublishRepository(db)
		msgRepo := mysql.NewMessageRepository(db)
		statusRepo := mysql.NewMessageStatusRepository(db)
		groupRepo := mysql.NewGroupRepository(db)
		userGroupRepo := mysql.NewUserGroupRepository(db)
		userRepo := mysql.NewUserRepository(db)
		outboxWriter := mysql.NewOutboxWriterRepository(db)

		// Initialize use cases
		msgUC, err := usecase.NewMessageUseCase(publishRepo, msgRepo, statusRepo, userGroupRepo, outboxWriter, snowflakeGen, hashRing)
		if err != nil {
			log.Fatalf("Failed to initialize MessageUseCase: %v", err)
			return
		}

		grpUC, err := usecase.NewGroupUseCase(groupRepo, userGroupRepo, hashRing, nc)
		if err != nil {
			log.Fatalf("Failed to initialize GroupUseCase: %v", err)
			return
		}

		profileUC, err := usecase.NewProfileUseCase(userRepo)
		if err != nil {
			log.Fatalf("Failed to initialize ProfileUseCase: %v", err)
			return
		}

		historyUC, err := usecase.NewHistoryUseCase(msgRepo, userGroupRepo)
		if err != nil {
			log.Fatalf("Failed to initialize HistoryUseCase: %v", err)
			return
		}

		authUC, err := usecase.NewAuthUseCase(userRepo, snowflakeGen, cfg.HTTP.JWTSecret)
		if err != nil {
			log.Fatalf("Failed to initialize AuthUseCase: %v", err)
			return
		}

		eventUC, err := usecase.NewEventUseCase(outboxWriter, userGroupRepo, snowflakeGen, hashRing)
		if err != nil {
			log.Fatalf("Failed to initialize EventUseCase: %v", err)
			return
		}

		// Create and start HTTP server
		server := httpsvr.New(&cfg.HTTP)

		// Register routes
		healthDeps := &httpsvr.HealthDeps{DB: db, RDB: rdb, NC: nc}
		server.RegisterRoutes(msgUC, grpUC, profileUC, historyUC, authUC, eventUC, cfg.App.ID, healthDeps)

		// Start server in background
		serverErr := make(chan error, 1)
		go func() {
			serverErr <- server.Start()
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		select {
		case err := <-serverErr:
			log.Errorf("Server error: %v", err)
		case <-quit:
			log.Info("HTTP Ingress server shutting down...")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				log.Errorf("Shutdown error: %v", err)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(httpCmd)
}
