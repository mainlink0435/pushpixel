package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Valid(t *testing.T) {
	yaml := `
directories:
  - /photos
db_path: /data/pushpixel.db
auth:
  client_id: abc
  client_secret: secret
`
	path := writeConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != "/photos" {
		t.Errorf("unexpected directories: %v", cfg.Directories)
	}
	if cfg.DBPath != "/data/pushpixel.db" {
		t.Errorf("unexpected db_path: %s", cfg.DBPath)
	}
	if cfg.Polling.Interval != 5*time.Minute {
		t.Errorf("expected default polling interval 5m, got %v", cfg.Polling.Interval)
	}
	if cfg.Retry.MaxAttempts != 5 {
		t.Errorf("expected default max_attempts 5, got %d", cfg.Retry.MaxAttempts)
	}
	if cfg.Upload.MaxConcurrent != 2 {
		t.Errorf("expected default max_concurrent 2, got %d", cfg.Upload.MaxConcurrent)
	}
	if cfg.WebUI.Host != "127.0.0.1" {
		t.Errorf("expected default host 127.0.0.1, got %s", cfg.WebUI.Host)
	}
	if cfg.WebUI.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.WebUI.Port)
	}
}

func TestLoad_OverridesDefaults(t *testing.T) {
	yaml := `
directories:
  - /a
  - /b
db_path: /custom/db.sqlite
polling:
  interval: 10m
auth:
  client_id: abc
  client_secret: secret
`
	path := writeConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Polling.Interval != 10*time.Minute {
		t.Errorf("expected 10m, got %v", cfg.Polling.Interval)
	}
	if len(cfg.Directories) != 2 {
		t.Errorf("expected 2 directories, got %d", len(cfg.Directories))
	}
	if cfg.DBPath != "/custom/db.sqlite" {
		t.Errorf("expected custom db_path, got %s", cfg.DBPath)
	}
	if len(cfg.FileExtensions) != 6 {
		t.Errorf("expected 6 default extensions, got %d", len(cfg.FileExtensions))
	}
}

func TestLoad_NoFileExtensions(t *testing.T) {
	path := writeConfig(t, `directories:
  - /photos
file_extensions:
auth:
  client_id: abc
  client_secret: secret
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing file_extensions")
	}
}

func TestLoad_NoDirectories(t *testing.T) {
	path := writeConfig(t, "auth:\n  client_id: abc\n  client_secret: secret\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing directories")
	}
}

func TestLoad_EmptyDirectory(t *testing.T) {
	path := writeConfig(t, "directories:\n  -\nauth:\n  client_id: abc\n  client_secret: secret\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestLoad_MissingClientID(t *testing.T) {
	path := writeConfig(t, "directories:\n  - /photos\nauth:\n  client_secret: secret\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing client_id")
	}
}

func TestLoad_MissingClientSecret(t *testing.T) {
	path := writeConfig(t, "directories:\n  - /photos\nauth:\n  client_id: abc\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing client_secret")
	}
}

func TestLoad_InvalidBatchSize(t *testing.T) {
	yaml := `
directories:
  - /photos
upload:
  batch_size: 100
auth:
  client_id: abc
  client_secret: secret
`
	path := writeConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for batch_size > 50")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	yaml := `
directories:
  - /photos
webui:
  port: 99999
auth:
  client_id: abc
  client_secret: secret
`
	path := writeConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestLoad_BadYAML(t *testing.T) {
	path := writeConfig(t, "{{{{invalid")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for bad YAML")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_NegativePollingInterval(t *testing.T) {
	yaml := `
directories:
  - /photos
polling:
  interval: -5m
auth:
  client_id: abc
  client_secret: secret
`
	path := writeConfig(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for negative interval")
	}
}
