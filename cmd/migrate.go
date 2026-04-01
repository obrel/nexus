package main

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/obrel/nexus/db"
	"github.com/obrel/nexus/internal/infra/logger"
	"github.com/spf13/cobra"
)

// newMigrator creates a migrate instance using the embedded SQL files.
func newMigrator() (*migrate.Migrate, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true",
		cfg.MySQL.User,
		cfg.MySQL.Password,
		cfg.MySQL.Host,
		cfg.MySQL.Port,
		cfg.MySQL.DBName,
	)

	src, err := iofs.New(db.MigrationsFS, "migrations/mysql")
	if err != nil {
		return nil, fmt.Errorf("failed to open embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, "mysql://"+dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrator: %w", err)
	}
	return m, nil
}

var (
	migrateCmd = &cobra.Command{
		Use:   "migrate",
		Short: "Database migration management",
	}

	migrateUpCmd = &cobra.Command{
		Use:   "up",
		Short: "Run all pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg == nil {
				return fmt.Errorf("configuration not initialized")
			}

			log := logger.For("infra", "migrate")

			m, err := newMigrator()
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Up(); err != nil {
				if err == migrate.ErrNoChange {
					log.Info("No pending migrations")
					return nil
				}
				return fmt.Errorf("migration failed: %w", err)
			}

			log.Info("Migrations completed successfully")
			return nil
		},
	}

	migrateDownCmd = &cobra.Command{
		Use:   "down",
		Short: "Rollback the last migration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg == nil {
				return fmt.Errorf("configuration not initialized")
			}

			log := logger.For("infra", "migrate")

			m, err := newMigrator()
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Down(); err != nil {
				if err == migrate.ErrNoChange {
					log.Info("No migrations to rollback")
					return nil
				}
				return fmt.Errorf("migration rollback failed: %w", err)
			}

			log.Info("Migration rolled back successfully")
			return nil
		},
	}

	migrateForceCmd = &cobra.Command{
		Use:   "force [version]",
		Short: "Force set migration version (clears dirty state)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg == nil {
				return fmt.Errorf("configuration not initialized")
			}

			log := logger.For("infra", "migrate")

			version := 0
			if _, err := fmt.Sscanf(args[0], "%d", &version); err != nil {
				return fmt.Errorf("invalid version: %s", args[0])
			}

			m, err := newMigrator()
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Force(version); err != nil {
				return fmt.Errorf("force version failed: %w", err)
			}

			log.Infof("Forced migration version to %d", version)
			return nil
		},
	}

	migrateVersionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show current migration version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfg == nil {
				return fmt.Errorf("configuration not initialized")
			}

			log := logger.For("infra", "migrate")

			m, err := newMigrator()
			if err != nil {
				return err
			}
			defer m.Close()

			version, dirty, err := m.Version()
			if err != nil {
				return fmt.Errorf("failed to get version: %w", err)
			}

			if dirty {
				log.Warnf("Current version: %d (DIRTY - migration in progress)", version)
			} else {
				log.Infof("Current version: %d", version)
			}

			return nil
		},
	}
)

func init() {
	migrateCmd.AddCommand(migrateUpCmd, migrateDownCmd, migrateForceCmd, migrateVersionCmd)
	rootCmd.AddCommand(migrateCmd)
}
