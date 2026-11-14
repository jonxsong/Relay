package config

import "testing"

func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing database error")
	}
}

func TestLoadRejectsProductionWithDevelopmentAuth(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://relay:secret@localhost/relay")
	t.Setenv("RELAY_ENV", "production")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want authentication safety error")
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
