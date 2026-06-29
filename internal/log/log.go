package log

import (
	"context"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/mainLink0435/pushpixel/internal/config"
)

var currentLogger *lumberjack.Logger

type fanoutHandler struct {
	handlers []slog.Handler
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, hdl := range h.handlers {
		if hdl.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, hdl := range h.handlers {
		if hdl.Enabled(ctx, r.Level) {
			_ = hdl.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	n := &fanoutHandler{}
	for _, hdl := range h.handlers {
		n.handlers = append(n.handlers, hdl.WithAttrs(attrs))
	}
	return n
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	n := &fanoutHandler{}
	for _, hdl := range h.handlers {
		n.handlers = append(n.handlers, hdl.WithGroup(name))
	}
	return n
}

func Setup(cfg config.LogConfig) error {
	level := parseLevel(cfg.Level)

	var handlers []slog.Handler

	if cfg.FilePath != "" {
		currentLogger = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
		}
		handlers = append(handlers, slog.NewJSONHandler(currentLogger, &slog.HandlerOptions{
			Level: level,
		}))
	}

	handlers = append(handlers, slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))

	switch len(handlers) {
	case 0:
		return nil
	case 1:
		slog.SetDefault(slog.New(handlers[0]))
	default:
		slog.SetDefault(slog.New(&fanoutHandler{handlers: handlers}))
	}

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
