package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/mainLink0435/pushpixel/internal/auth"
	"github.com/mainLink0435/pushpixel/internal/config"
	"github.com/mainLink0435/pushpixel/internal/db"
	"github.com/mainLink0435/pushpixel/internal/log"
	"github.com/mainLink0435/pushpixel/internal/monitor"
	"github.com/mainLink0435/pushpixel/internal/sync"
	"github.com/mainLink0435/pushpixel/internal/webui"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	if err := log.Setup(cfg.Logging); err != nil {
		fmt.Fprintf(os.Stderr, "logger: %v\n", err)
		os.Exit(1)
	}

	slog.Info("pushpixel starting",
		"version", version,
		"go", runtime.Version(),
		"directories", cfg.Directories,
		"db", cfg.DBPath,
	)

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		slog.Error("database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	a, err := auth.NewAuth(cfg.Auth)
	if err != nil {
		slog.Error("auth init", "error", err)
		os.Exit(1)
	}

	srv := webui.New(a, cfg.WebUI, database)
	go func() {
		if err := srv.Start(); err != nil {
			slog.Error("webui", "error", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := monitor.New(cfg.Directories, cfg.FileExtensions, cfg.Polling.Interval, database)
	scanCh := m.Start(ctx)

	fileCh := make(chan string, 100)

	client := a.HTTPClient(context.Background())
	uploader := sync.NewUploader(client, cfg.Upload)
	engine := sync.NewEngine(*cfg, database, uploader, srv, a.IsAuthenticated)
	go func() {
		if err := engine.Run(ctx, fileCh); err != nil && err != context.Canceled {
			slog.Error("sync engine", "error", err)
		}
	}()

	go feedFiles(ctx, database, fileCh, scanCh)

	if a.IsAuthenticated() {
		slog.Info("authenticated with Google Photos")
	} else {
		slog.Warn("not authenticated — visit the web UI to sign in")
	}

	slog.Info("pushpixel ready",
		"webui", fmt.Sprintf("http://%s:%d", cfg.WebUI.Host, cfg.WebUI.Port),
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)

	cancel()
	log.Close()
	time.Sleep(500 * time.Millisecond)
}

func feedFiles(ctx context.Context, database *db.DB, fileCh chan<- string, scanCh <-chan struct{}) {
	for {
		pending, err := database.ListPendingLimit(100)
		if err != nil {
			slog.Error("feed pending files", "error", err)
			select {
			case <-ctx.Done():
				close(fileCh)
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}

		for _, f := range pending {
			select {
			case <-ctx.Done():
				close(fileCh)
				return
			case fileCh <- f.AbsolutePath:
			}
		}

		if len(pending) == 0 {
			select {
			case <-ctx.Done():
				close(fileCh)
				return
			case <-scanCh:
			case <-time.After(5 * time.Minute):
			}
		}
	}
}
