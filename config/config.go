package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config is the root application configuration structure.
type Config struct {
	App    AppConfig    `mapstructure:"app"`
	HTTP   HTTPConfig   `mapstructure:"http"`
	WS     WSConfig     `mapstructure:"ws"`
	MySQL  MySQLConfig  `mapstructure:"mysql"`
	Redis  RedisConfig  `mapstructure:"redis"`
	NATS   NATSConfig   `mapstructure:"nats"`
	Log    LogConfig    `mapstructure:"log"`
	Worker WorkerConfig `mapstructure:"worker"`
	MCP    MCPConfig    `mapstructure:"mcp"`
}

// AppConfig holds general application metadata.
type AppConfig struct {
	ID      string `mapstructure:"id"`
	Env     string `mapstructure:"env"`
	Version string `mapstructure:"version"`
}

// HTTPConfig configures the HTTP ingress server.
type HTTPConfig struct {
	Port           int    `mapstructure:"port"`
	JWTSecret      string `mapstructure:"jwt_secret"`
	InternalAPIKey string `mapstructure:"internal_api_key"` // shared secret for internal service-to-service calls
	MaxBodySize    int64  `mapstructure:"max_body_size"`    // bytes; 0 = use default (1MB)
	MaxHeaderBytes int    `mapstructure:"max_header_bytes"` // bytes; 0 = use default (1MB)
}

// WSConfig configures the WebSocket server.
type WSConfig struct {
	Port        int `mapstructure:"port"`
	PingPeriod  int `mapstructure:"ping_period"`  // seconds; 0 = default (30s)
	PongWait    int `mapstructure:"pong_wait"`    // seconds; 0 = default (90s)
	WriteWait   int `mapstructure:"write_wait"`   // seconds; 0 = default (10s)
	PresenceTTL int `mapstructure:"presence_ttl"` // seconds; 0 = default (60s)
}

// MySQLConfig contains connection parameters for MySQL.
type MySQLConfig struct {
	Host            string `mapstructure:"host"`
	Port            int    `mapstructure:"port"`
	User            string `mapstructure:"user"`
	Password        string `mapstructure:"password"`
	DBName          string `mapstructure:"dbname"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"` // seconds
}

// RedisConfig contains connection parameters for Redis.
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
}

// NATSConfig contains NATS broker connection settings.
type NATSConfig struct {
	URL                  string `mapstructure:"url"`
	SubPendingMsgsLimit  int    `mapstructure:"sub_pending_msgs_limit"`  // 0 = default (65536)
	SubPendingBytesLimit int    `mapstructure:"sub_pending_bytes_limit"` // bytes; 0 = default (64MB)
}

// LogConfig specifies logging behaviors.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// WorkerConfig configures the outbox relay worker.
type WorkerConfig struct {
	BatchSize    int `mapstructure:"batch_size"`    // entries per poll; 0 = default (50)
	PollInterval int `mapstructure:"poll_interval"` // milliseconds; 0 = default (1000)
}

// MCPConfig configures the optional MCP server.
type MCPConfig struct {
	Port int `mapstructure:"port"`
}

// PresenceTTLSeconds returns the configured presence TTL in seconds, defaulting to 60.
func (c *WSConfig) PresenceTTLSeconds() int {
	if c.PresenceTTL > 0 {
		return c.PresenceTTL
	}
	return 60
}

// LoadConfig reads configuration from file or environment variables.
func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Defaults
	viper.SetDefault("app.env", "development")
	viper.SetDefault("http.port", 8080)
	viper.SetDefault("http.jwt_secret", "dev-secret-key-change-in-production")
	viper.SetDefault("http.internal_api_key", "dev-internal-key-change-in-production")
	viper.SetDefault("http.max_body_size", 1<<20) // 1MB
	viper.SetDefault("http.max_header_bytes", 1<<20) // 1MB
	viper.SetDefault("ws.port", 8081)
	viper.SetDefault("ws.ping_period", 30)
	viper.SetDefault("ws.pong_wait", 90)
	viper.SetDefault("ws.write_wait", 10)
	viper.SetDefault("ws.presence_ttl", 60)
	viper.SetDefault("nats.sub_pending_msgs_limit", 65536)
	viper.SetDefault("nats.sub_pending_bytes_limit", 67108864) // 64MB
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "text")
	viper.SetDefault("mysql.max_open_conns", 100)
	viper.SetDefault("mysql.max_idle_conns", 25)
	viper.SetDefault("mysql.conn_max_lifetime", 300) // 5 minutes
	viper.SetDefault("worker.batch_size", 50)
	viper.SetDefault("worker.poll_interval", 1000) // 1s
	viper.SetDefault("mcp.port", 8082)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate checks configuration for production-critical requirements.
// It always validates structural constraints and enforces stricter rules in production.
func (c *Config) Validate() error {
	const defaultSecret = "dev-secret-key-change-in-production"

	// --- Always enforced ---

	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		return fmt.Errorf("config: http.port must be between 1 and 65535, got %d", c.HTTP.Port)
	}
	if c.WS.Port <= 0 || c.WS.Port > 65535 {
		return fmt.Errorf("config: ws.port must be between 1 and 65535, got %d", c.WS.Port)
	}
	if c.HTTP.Port == c.WS.Port {
		return fmt.Errorf("config: http.port and ws.port must differ (both are %d)", c.HTTP.Port)
	}
	if c.MySQL.MaxOpenConns > 0 && c.MySQL.MaxIdleConns > c.MySQL.MaxOpenConns {
		return fmt.Errorf("config: mysql.max_idle_conns (%d) must not exceed mysql.max_open_conns (%d)",
			c.MySQL.MaxIdleConns, c.MySQL.MaxOpenConns)
	}
	if c.HTTP.MaxBodySize < 0 {
		return fmt.Errorf("config: http.max_body_size must be non-negative, got %d", c.HTTP.MaxBodySize)
	}
	if c.Worker.BatchSize < 0 {
		return fmt.Errorf("config: worker.batch_size must be non-negative, got %d", c.Worker.BatchSize)
	}
	if c.Worker.PollInterval < 0 {
		return fmt.Errorf("config: worker.poll_interval must be non-negative, got %d", c.Worker.PollInterval)
	}

	// --- Production-only ---

	if c.App.Env == "production" {
		if c.HTTP.InternalAPIKey == "" {
			return fmt.Errorf("config: http.internal_api_key is required in production")
		}
		if len(c.HTTP.InternalAPIKey) < 32 {
			return fmt.Errorf("config: http.internal_api_key must be at least 32 characters in production")
		}
		if c.HTTP.JWTSecret == "" || c.HTTP.JWTSecret == defaultSecret {
			return fmt.Errorf("config: http.jwt_secret must be explicitly set in production")
		}
		if len(c.HTTP.JWTSecret) < 32 {
			return fmt.Errorf("config: http.jwt_secret must be at least 32 characters in production")
		}
		if c.MySQL.Host == "" {
			return fmt.Errorf("config: mysql.host is required in production")
		}
		if c.MySQL.DBName == "" {
			return fmt.Errorf("config: mysql.dbname is required in production")
		}
		if c.Redis.Host == "" {
			return fmt.Errorf("config: redis.host is required in production")
		}
		if c.NATS.URL == "" {
			return fmt.Errorf("config: nats.url is required in production")
		}
		if c.MCP.Port == 0 {
			return fmt.Errorf("config: mcp.port must be set in production")
		}
		if c.MCP.Port <= 0 || c.MCP.Port > 65535 {
			return fmt.Errorf("config: mcp.port must be between 1 and 65535, got %d", c.MCP.Port)
		}
		if c.MCP.Port == c.HTTP.Port {
			return fmt.Errorf("config: mcp.port and http.port must differ (both are %d)", c.MCP.Port)
		}
		if c.MCP.Port == c.WS.Port {
			return fmt.Errorf("config: mcp.port and ws.port must differ (both are %d)", c.MCP.Port)
		}
	}

	return nil
}
