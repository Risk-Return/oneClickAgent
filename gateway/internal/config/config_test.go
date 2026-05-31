package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	os.Setenv("IAGENT_DB_URL", "postgres://test:test@localhost:5432/test")
	os.Setenv("IAGENT_JWT_SECRET", "test-secret-key-that-is-32-chars-long")
	defer os.Unsetenv("IAGENT_DB_URL")
	defer os.Unsetenv("IAGENT_JWT_SECRET")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("expected :8080, got %s", cfg.HTTPAddr)
	}
	if cfg.AccessTTL != 15*time.Minute {
		t.Errorf("expected 15m, got %v", cfg.AccessTTL)
	}
	if cfg.RefreshTTL != 720*time.Hour {
		t.Errorf("expected 720h, got %v", cfg.RefreshTTL)
	}
	if cfg.MaxUploadMB != 100 {
		t.Errorf("expected 100, got %d", cfg.MaxUploadMB)
	}
	if cfg.HeartbeatS != 15 {
		t.Errorf("expected 15, got %d", cfg.HeartbeatS)
	}
	if cfg.QueueTTL != 1*time.Hour {
		t.Errorf("expected 1h, got %v", cfg.QueueTTL)
	}
	if cfg.MaxQueuedPerUser != 10 {
		t.Errorf("expected 10, got %d", cfg.MaxQueuedPerUser)
	}
}

func TestLoadMissingDBURL(t *testing.T) {
	os.Setenv("IAGENT_JWT_SECRET", "test-secret-key-that-is-32-chars-long")
	os.Unsetenv("IAGENT_DB_URL")
	defer os.Unsetenv("IAGENT_JWT_SECRET")

	_, err := Load()
	if err == nil {
		t.Error("expected error for missing IAGENT_DB_URL")
	}
}

func TestLoadMissingJWTSecret(t *testing.T) {
	os.Setenv("IAGENT_DB_URL", "postgres://test:test@localhost:5432/test")
	os.Unsetenv("IAGENT_JWT_SECRET")
	defer os.Unsetenv("IAGENT_DB_URL")

	_, err := Load()
	if err == nil {
		t.Error("expected error for missing IAGENT_JWT_SECRET")
	}
}

func TestLoadShortJWTSecret(t *testing.T) {
	os.Setenv("IAGENT_DB_URL", "postgres://test:test@localhost:5432/test")
	os.Setenv("IAGENT_JWT_SECRET", "short")
	defer os.Unsetenv("IAGENT_DB_URL")
	defer os.Unsetenv("IAGENT_JWT_SECRET")

	_, err := Load()
	if err == nil {
		t.Error("expected error for short IAGENT_JWT_SECRET")
	}
}

func TestDerivedValues(t *testing.T) {
	os.Setenv("IAGENT_DB_URL", "postgres://test:test@localhost:5432/test")
	os.Setenv("IAGENT_JWT_SECRET", "test-secret-key-that-is-32-chars-plus")
	os.Setenv("IAGENT_MAX_UPLOAD_MB", "50")
	defer os.Unsetenv("IAGENT_DB_URL")
	defer os.Unsetenv("IAGENT_JWT_SECRET")
	defer os.Unsetenv("IAGENT_MAX_UPLOAD_MB")

	cfg, _ := Load()
	if cfg.MaxUploadBytes != 50*1024*1024 {
		t.Errorf("expected 50MB, got %d", cfg.MaxUploadBytes)
	}
	if cfg.HeartbeatMissThreshold != 45*time.Second {
		t.Errorf("expected 45s (3x15s), got %v", cfg.HeartbeatMissThreshold)
	}
}
