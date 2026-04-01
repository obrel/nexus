package mysql

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/obrel/nexus/config"
	"github.com/obrel/nexus/internal/infra/logger"

	_ "github.com/go-sql-driver/mysql"
)

// New creates a MySQL connection pool with the given configuration.
func New(cfg *config.MySQLConfig) (*sql.DB, error) {
	log := logger.For("infra", "mysql")

	// Build DSN (Data Source Name)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=UTC&tls=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
		cfg.TLSMode,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open MySQL: %w", err)
	}

	// Configure connection pool from config (Viper guarantees non-zero defaults)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping MySQL: %w", err)
	}

	log.Infof("Connected to MySQL at %s:%d/%s", cfg.Host, cfg.Port, cfg.DBName)
	return db, nil
}
