package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/obrel/nexus/internal/delivery/ws"
	"github.com/obrel/nexus/internal/egress"
	"github.com/obrel/nexus/internal/infra/hash"
	natsclient "github.com/obrel/nexus/internal/infra/nats"
	redisinfra "github.com/obrel/nexus/internal/infra/redis"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/spf13/cobra"
)

var wsCmd = &cobra.Command{
	Use:   "ws",
	Short: "Start the Nexus Egress WebSocket server",
	Run: func(cmd *cobra.Command, args []string) {
		if cfg == nil {
			logger.Fatal("Configuration not initialized - ensure config file exists")
			return
		}

		log := logger.For("delivery", "ws")

		// Redis
		rdb, err := redisinfra.New(&cfg.Redis)
		if err != nil {
			log.Fatalf("Failed to connect to Redis: %v", err)
		}
		defer rdb.Close()

		// Presence repository
		presenceRepo := redisinfra.NewPresenceRepository(rdb)

		// NATS — for presence online/offline events
		nc, err := natsclient.New(&cfg.NATS)
		if err != nil {
			log.Fatalf("Failed to connect to NATS: %v", err)
		}
		defer nc.Drain()

		// Node ID — use cfg.App.ID if set, otherwise fall back to hostname:port.
		nodeID := cfg.App.ID
		if nodeID == "" {
			hostname, _ := os.Hostname()
			nodeID = fmt.Sprintf("%s:%d", hostname, cfg.WS.Port)
		}

		// Connection manager
		mgr := egress.NewConnManager(presenceRepo, nc, nodeID)
		mgr.SetPresenceTTL(cfg.WS.PresenceTTLSeconds())
		mgr.SetSubPendingLimits(cfg.NATS.SubPendingMsgsLimit, cfg.NATS.SubPendingBytesLimit)

		// Group registry — shard-based wildcard subscriptions for group messaging.
		ring := hash.New(hash.DefaultShards)
		registry := egress.NewGroupRegistry(mgr, ring, nc)
		registry.SetSubPendingLimits(cfg.NATS.SubPendingMsgsLimit, cfg.NATS.SubPendingBytesLimit)

		// Register GroupRegistry as the control handler so HTTP Ingress
		// group join/leave actions are reflected in local NATS subscriptions.
		mgr.SetControlHandler(registry)

		// Clean up group memberships when a user disconnects.
		mgr.OnDisconnect(func(appID, userID string) {
			registry.LeaveAll(appID, userID)
		})

		// WebSocket handler — reuses the same JWT secret as the HTTP tier.
		wsHandler := ws.NewHandler(cfg.HTTP.JWTSecret, mgr, nc, ring, &cfg.WS)

		// HTTP mux: all requests go to the WS upgrade handler.
		mux := http.NewServeMux()
		mux.Handle("/", wsHandler)

		srv := ws.NewServer(cfg.WS.Port, mux)

		log.Infof("Starting WebSocket Egress server on port %d (node=%s)", cfg.WS.Port, nodeID)

		go func() {
			if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("WebSocket server error: %v", err)
			}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		log.Info("WebSocket Egress server shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		mgr.CloseAll()

		if err := srv.Shutdown(ctx); err != nil {
			log.Errorf("WebSocket server shutdown error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(wsCmd)
}
