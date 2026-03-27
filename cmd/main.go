package main

import (
	"fmt"
	"os"

	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "nexus",
	Short: "Nexus is a high-concurrency scalable WebSocket service",
	Long: `Nexus is a distributed, high-concurrency messaging service designed to scale
gracefully to millions of concurrent WebSocket connections.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if err := cfg.Validate(); err != nil {
			return err
		}

		// Initialize logger
		logger.SetLevel(logger.InfoLevel) // Default, can be overridden by config
		if cfg.Log.Level != "" {
			switch cfg.Log.Level {
			case "debug":
				logger.SetLevel(logger.DebugLevel)
			case "info":
				logger.SetLevel(logger.InfoLevel)
			case "warn":
				logger.SetLevel(logger.WarnLevel)
			case "error":
				logger.SetLevel(logger.ErrorLevel)
			}
		}

		if cfg.Log.Format == "json" {
			logger.SetFormat(logger.JSONFormat)
		}

		logger.Infof("Nexus starting (env: %s, version: %s)", cfg.App.Env, cfg.App.Version)
		return nil
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
