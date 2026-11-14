package config

import "testing"

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing database error")
	}
}

func TestLoadRejectsProductionWithoutAPIKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://relay:secret@localhost/relay")
	t.Setenv("RELAY_ENV", "production")
	t.Setenv("RELAY_API_KEY", "")
	t.Setenv("RELAY_API_KEY_FILE", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing API key error")
	}
}

func TestLoadAcceptsProductionWithStrongAPIKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://relay:secret@localhost/relay")
	t.Setenv("RELAY_ENV", "production")
	t.Setenv("RELAY_API_KEY", "relay_test_0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIKey == "" {
		t.Fatal("APIKey is empty")
	}
}

func TestLoadUsesBoundedDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://relay:secret@localhost/relay")
	t.Setenv("RELAY_ENV", "development")
	t.Setenv("RELAY_MAX_REQUEST_BYTES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxRequestBytes != 1<<20 {
		t.Fatalf("MaxRequestBytes = %d, want %d", cfg.MaxRequestBytes, 1<<20)
	}
}
