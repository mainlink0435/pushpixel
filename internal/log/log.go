package log

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/mainLink0435/pushpixel/internal/config"
)

var currentLogger *lumberjack.Logger

func Setup(cfg config.LogConfig) error {
	level := parseLevel(cfg.Level)

	var writers []io.Writer

	if cfg.FilePath != "" {
		currentLogger = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
		}
		writers = append(writers, currentLogger)
	}

	if os.Getenv("PUSHPIXEL_QUIET") == "" {
		writers = append(writers, os.Stdout)
	}

	var writer io.Writer
	switch len(writers) {
	case 0:
		return nil
	case 1:
		writer = writers[0]
	default:
		writer = io.MultiWriter(writers...)
	}

	jsonHandler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(jsonHandler))

	return nil
}

func Close() error {
	if currentLogger != nil {
		return currentLogger.Close()
	}
	return nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
