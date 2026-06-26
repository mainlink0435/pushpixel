package log

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mainLink0435/pushpixel/internal/config"
)

func saveSlogHandler() func() {
	old := slog.Default().Handler()
	return func() {
		Close()
		slog.SetDefault(slog.New(old))
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"invalid", slog.LevelInfo},
		{"DEBUG", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
	}

	for _, tt := range tests {
		got := parseLevel(tt.input)
		if got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSetup_FileOutput(t *testing.T) {
	defer saveSlogHandler()()

	dir, err := os.MkdirTemp("", "log_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	logPath := filepath.Join(dir, "test.log")
	cfg := config.LogConfig{
		Level:      "info",
		FilePath:   logPath,
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
	}

	t.Setenv("PUSHPIXEL_QUIET", "1")

	if err := Setup(cfg); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	slog.Info("test message", "key", "value")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("parse JSON: %v\nraw: %s", err, string(data))
	}

	if msg, _ := entry["msg"].(string); msg != "test message" {
		t.Errorf("expected msg 'test message', got %v", entry["msg"])
	}
	if lvl, _ := entry["level"].(string); lvl != "INFO" {
		t.Errorf("expected level INFO, got %v", entry["level"])
	}
	if v, _ := entry["key"].(string); v != "value" {
		t.Errorf("expected key=value, got key=%v", entry["key"])
	}
}

func TestSetup_LevelFiltering(t *testing.T) {
	defer saveSlogHandler()()

	dir, err := os.MkdirTemp("", "log_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	logPath := filepath.Join(dir, "filter.log")
	cfg := config.LogConfig{
		Level:      "error",
		FilePath:   logPath,
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
	}

	t.Setenv("PUSHPIXEL_QUIET", "1")

	if err := Setup(cfg); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	slog.Debug("debug msg")
	slog.Info("info msg")
	slog.Warn("warn msg")
	slog.Error("error msg")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 log line (error), got %d:\n%s", len(lines), string(data))
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	if msg, _ := entry["msg"].(string); msg != "error msg" {
		t.Errorf("expected 'error msg', got %v", entry["msg"])
	}
}

func TestSetup_QuietAndNoFile(t *testing.T) {
	defer saveSlogHandler()()

	t.Setenv("PUSHPIXEL_QUIET", "1")

	cfg := config.LogConfig{
		Level:      "info",
		FilePath:   "",
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
	}

	if err := Setup(cfg); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestSetup_StdoutOnly(t *testing.T) {
	defer saveSlogHandler()()

	cfg := config.LogConfig{
		Level:      "info",
		FilePath:   "",
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
	}

	if err := Setup(cfg); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestSetup_InvalidLevelDefaults(t *testing.T) {
	defer saveSlogHandler()()

	dir, err := os.MkdirTemp("", "log_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	logPath := filepath.Join(dir, "default.log")
	cfg := config.LogConfig{
		Level:      "banana",
		FilePath:   logPath,
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
	}

	t.Setenv("PUSHPIXEL_QUIET", "1")

	if err := Setup(cfg); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	slog.Info("level should default to info")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var found bool
	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if msg, _ := entry["msg"].(string); msg == "level should default to info" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected info-level message to appear")
	}
}

func TestSetup_ConcurrentChanges(t *testing.T) {
	defer saveSlogHandler()()

	t.Setenv("PUSHPIXEL_QUIET", "1")

	for i := 0; i < 10; i++ {
		dir, err := os.MkdirTemp("", "log_test")
		if err != nil {
			t.Fatal(err)
		}

		logPath := filepath.Join(dir, "test.log")
		cfg := config.LogConfig{
			Level:      "info",
			FilePath:   logPath,
			MaxSize:    10,
			MaxBackups: 1,
			MaxAge:     1,
		}

		if err := Setup(cfg); err != nil {
			t.Fatalf("Setup: %v", err)
		}

		slog.Info("sequential msg")

		Close()
		os.RemoveAll(dir)
	}
}
