package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	mcp "github.com/obrel/nexus/internal/delivery/mcp"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the Nexus MCP control server",
	Run: func(cmd *cobra.Command, args []string) {
		if cfg == nil {
			logger.Fatal("Configuration not initialized - ensure config file exists")
			return
		}

		log := logger.For("delivery", "mcp")

		server := mcp.New(&cfg.MCP)
		server.RegisterRoutes()

		serverErr := make(chan error, 1)
		go func() {
			serverErr <- server.Start()
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		select {
		case err := <-serverErr:
			log.Errorf("MCP server error: %v", err)
		case <-quit:
			log.Info("MCP server shutting down...")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				log.Errorf("MCP server shutdown error: %v", err)
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
