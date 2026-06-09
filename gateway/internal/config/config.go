// Package config loads and validates the gateway configuration from
// environment variables (caarlos0/env) and optional .env file.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds all gateway configuration.
type Config struct {
	HTTPAddr            string        `env:"IAGENT_HTTP_ADDR" envDefault:":8080"`
	TLSCert             string        `env:"IAGENT_TLS_CERT"`
	TLSKey              string        `env:"IAGENT_TLS_KEY"`
	DBURL               string        `env:"IAGENT_DB_URL"`
	JWTSecret           string        `env:"IAGENT_JWT_SECRET"`
	AccessTTL           time.Duration `env:"IAGENT_ACCESS_TTL" envDefault:"15m"`
	RefreshTTL          time.Duration `env:"IAGENT_REFRESH_TTL" envDefault:"720h"`
	FileStore           string        `env:"IAGENT_FILE_STORE" envDefault:"local:/var/iagent/files"`
	MaxUploadMB         int64         `env:"IAGENT_MAX_UPLOAD_MB" envDefault:"100"`
	HeartbeatS          int           `env:"IAGENT_HEARTBEAT_S" envDefault:"15"`
	AgentsPerUser       int           `env:"IAGENT_AGENTS_PER_USER" envDefault:"1"`
	QueueTTL            time.Duration `env:"IAGENT_QUEUE_TTL" envDefault:"1h"`
	MaxQueuedPerUser    int           `env:"IAGENT_MAX_QUEUED_PER_USER" envDefault:"10"`
	MaxUploadBytes      int64         `env:"-"`
	HeartbeatInterval   time.Duration `env:"-"`
	HeartbeatMissThreshold time.Duration `env:"-"`
	ShutdownTimeout     time.Duration `env:"IAGENT_SHUTDOWN_TIMEOUT" envDefault:"30s"`
	MetricsAddr         string        `env:"IAGENT_METRICS_ADDR" envDefault:":9090"`
	LogLevel            string        `env:"IAGENT_LOG_LEVEL" envDefault:"info"`
	LogFormat           string        `env:"IAGENT_LOG_FORMAT" envDefault:"json"`
	Env                 string        `env:"IAGENT_ENV" envDefault:"development"`
	CORSAllowedOrigins  []string      `env:"IAGENT_CORS_ORIGINS" envSeparator:","`
	RateLimitAuthPerMin int           `env:"IAGENT_RATE_LIMIT_AUTH_PER_MIN" envDefault:"10"`
	RateLimitAPIPerSec  int           `env:"IAGENT_RATE_LIMIT_API_PER_SEC" envDefault:"100"`
	RateLimitJobSubmitPerMin int    `env:"IAGENT_RATE_LIMIT_JOB_SUBMIT_PER_MIN" envDefault:"30"`
	WSMaxSubscriptions     int           `env:"IAGENT_WS_MAX_SUBSCRIPTIONS" envDefault:"50"`
	FileRetentionHours     int           `env:"IAGENT_FILE_RETENTION_HOURS" envDefault:"24"`
	JobResultRetentionDays int           `env:"IAGENT_JOB_RESULT_RETENTION_DAYS" envDefault:"90"`
	WebDistDir             string        `env:"IAGENT_WEB_DIST_DIR"`

	// VNC session config
	VNCIdleTTLSecs       int    `env:"IAGENT_VNC_IDLE_TTL" envDefault:"300"`
	VNCMaxTTLSecs        int    `env:"IAGENT_VNC_MAX_TTL" envDefault:"1800"`
	VNCMaxSessionsPerUser int   `env:"IAGENT_VNC_MAX_SESSIONS_PER_USER" envDefault:"2"`
	VNCSessionBufBytes   int64  `env:"IAGENT_VNC_SESSION_BUF_BYTES" envDefault:"4194304"` // 4 MiB

	// Credential vault config
	CredKey string `env:"IAGENT_CRED_KEY"`   // base64 AES-256 data key
	CredKMS string `env:"IAGENT_CRED_KMS"`   // KMS key id (envelope encryption)
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse config: %w", err)
	}

	cfg.MaxUploadBytes = cfg.MaxUploadMB * 1024 * 1024
	cfg.HeartbeatInterval = time.Duration(cfg.HeartbeatS) * time.Second
	cfg.HeartbeatMissThreshold = cfg.HeartbeatInterval * 3

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// MustLoad loads config or panics.
func MustLoad() Config {
	cfg, err := Load()
	if err != nil {
		panic(err)
	}
	return cfg
}

func (c Config) validate() error {
	var errs []string

	if c.DBURL == "" {
		errs = append(errs, "IAGENT_DB_URL is required")
	}
	if c.DBURL != "" && !strings.Contains(c.DBURL, "sslmode=") {
		if c.Env != "development" {
			errs = append(errs, "IAGENT_DB_URL must include sslmode parameter (e.g. sslmode=require, sslmode=verify-full)")
		}
	}
	if c.JWTSecret == "" {
		errs = append(errs, "IAGENT_JWT_SECRET is required")
	}
	if c.JWTSecret != "" && len(c.JWTSecret) < 32 {
		errs = append(errs, "IAGENT_JWT_SECRET must be at least 32 characters")
	}
	if c.HeartbeatS < 5 {
		errs = append(errs, "IAGENT_HEARTBEAT_S must be at least 5")
	}
	if c.MaxUploadMB < 1 {
		errs = append(errs, "IAGENT_MAX_UPLOAD_MB must be at least 1")
	}
	if c.AgentsPerUser < 1 {
		errs = append(errs, "IAGENT_AGENTS_PER_USER must be at least 1")
	}
	if c.QueueTTL < time.Second {
		errs = append(errs, "IAGENT_QUEUE_TTL must be at least 1s")
	}
	if c.MaxQueuedPerUser < 1 {
		errs = append(errs, "IAGENT_MAX_QUEUED_PER_USER must be at least 1")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
