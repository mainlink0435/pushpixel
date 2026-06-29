package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Directories    []string       `yaml:"directories"`
	FileExtensions []string       `yaml:"file_extensions"`
	DBPath         string         `yaml:"db_path"`
	Polling        PollingConfig  `yaml:"polling"`
	Retry          RetryConfig    `yaml:"retry"`
	Upload         UploadConfig   `yaml:"upload"`
	StorageFull    StorageConfig  `yaml:"storage_full"`
	Logging        LogConfig      `yaml:"logging"`
	WebUI          WebUIConfig    `yaml:"webui"`
	Auth           AuthConfig     `yaml:"auth"`
}

type PollingConfig struct {
	Interval time.Duration `yaml:"interval"`
}

type RetryConfig struct {
	MaxAttempts int           `yaml:"max_attempts"`
	BaseDelay   time.Duration `yaml:"base_delay"`
	MaxDelay    time.Duration `yaml:"max_delay"`
	Jitter      bool          `yaml:"jitter"`
}

type UploadConfig struct {
	MaxConcurrent int `yaml:"max_concurrent"`
	BatchSize     int `yaml:"batch_size"`
}

type StorageConfig struct {
	Backoff time.Duration `yaml:"backoff"`
}

type LogConfig struct {
	Level      string `yaml:"level"`
	FilePath   string `yaml:"file_path"`
	MaxSize    int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAge     int    `yaml:"max_age_days"`
}

type WebUIConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type AuthConfig struct {
	ClientID      string `yaml:"client_id"`
	ClientSecret  string `yaml:"client_secret"`
	TokenDir      string `yaml:"token_dir"`
}

func defaults() *Config {
	dbPath := "pushpixel.db"
	logPath := "pushpixel.log"
	tokenDir := "."
	dirs := []string{}
	webPort := 8080

	if os.Getenv("PUSHPIXEL_DOCKER") == "1" {
		dbPath = "/app/data/pushpixel.db"
		logPath = "/app/data/pushpixel.log"
		tokenDir = "/app/data/"
		dirs = []string{"/photos"}
		webPort = 1978
	}

	return &Config{
		Directories:    dirs,
		FileExtensions: []string{".jpg", ".jpeg", ".png", ".webp", ".mp4", ".mov"},
		DBPath:         dbPath,
		Polling: PollingConfig{
			Interval: 5 * time.Minute,
		},
		Retry: RetryConfig{
			MaxAttempts: 5,
			BaseDelay:   1 * time.Second,
			MaxDelay:    30 * time.Second,
			Jitter:      true,
		},
		Upload: UploadConfig{
			MaxConcurrent: 2,
			BatchSize:     50,
		},
		StorageFull: StorageConfig{
			Backoff: 4 * time.Hour,
		},
		Logging: LogConfig{
			Level:      "info",
			FilePath:   logPath,
			MaxSize:    10,
			MaxBackups: 3,
			MaxAge:     30,
		},
		WebUI: WebUIConfig{
			Host: "127.0.0.1",
			Port: webPort,
		},
		Auth: AuthConfig{
			TokenDir: tokenDir,
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if len(c.Directories) == 0 {
		return fmt.Errorf("at least one directory must be specified")
	}

	for _, d := range c.Directories {
		if d == "" {
			return fmt.Errorf("directory path must not be empty")
		}
	}

	if len(c.FileExtensions) == 0 {
		return fmt.Errorf("at least one file extension must be specified")
	}
	for _, e := range c.FileExtensions {
		if e == "" {
			return fmt.Errorf("file extension must not be empty")
		}
	}

	if c.DBPath == "" {
		return fmt.Errorf("db_path must not be empty")
	}

	if c.Polling.Interval <= 0 {
		return fmt.Errorf("polling.interval must be positive")
	}

	if c.Retry.MaxAttempts <= 0 {
		return fmt.Errorf("retry.max_attempts must be positive")
	}
	if c.Retry.BaseDelay <= 0 {
		return fmt.Errorf("retry.base_delay must be positive")
	}
	if c.Retry.MaxDelay <= 0 {
		return fmt.Errorf("retry.max_delay must be positive")
	}

	if c.Upload.MaxConcurrent <= 0 {
		return fmt.Errorf("upload.max_concurrent must be positive")
	}
	if c.Upload.BatchSize <= 0 || c.Upload.BatchSize > 50 {
		return fmt.Errorf("upload.batch_size must be between 1 and 50")
	}

	if c.StorageFull.Backoff <= 0 {
		return fmt.Errorf("storage_full.backoff must be positive")
	}

	if c.WebUI.Port <= 0 || c.WebUI.Port > 65535 {
		return fmt.Errorf("webui.port must be between 1 and 65535")
	}

	if c.Auth.ClientID == "" {
		return fmt.Errorf("auth.client_id must not be empty")
	}
	if c.Auth.ClientSecret == "" {
		return fmt.Errorf("auth.client_secret must not be empty")
	}

	return nil
}
