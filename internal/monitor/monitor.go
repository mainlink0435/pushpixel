package monitor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mainLink0435/pushpixel/internal/db"
)

type Monitor struct {
	dirs       []string
	extensions map[string]bool
	interval   time.Duration
	database   *db.DB
}

func New(dirs []string, extensions []string, interval time.Duration, database *db.DB) *Monitor {
	extSet := make(map[string]bool, len(extensions))
	for _, e := range extensions {
		extSet[strings.ToLower(e)] = true
	}

	return &Monitor{
		dirs:       dirs,
		extensions: extSet,
		interval:   interval,
		database:   database,
	}
}

func (m *Monitor) Start(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go m.loop(ctx, ch)
	return ch
}

func (m *Monitor) loop(ctx context.Context, ch chan<- struct{}) {
	defer close(ch)

	m.scan(ch)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scan(ch)
		}
	}
}

func (m *Monitor) scan(ch chan<- struct{}) {
	slog.Info("scan started")
	scanStart := time.Now()

	var count int
	for _, dir := range m.dirs {
		count += m.walkDir(dir)
	}

	purged, _ := m.database.PurgeUnseenFiles(scanStart)
	count += purged

	total, _ := m.database.TotalCount()
	slog.Info("scan complete", "files_found", count, "total_tracked", total)

	select {
	case ch <- struct{}{}:
	default:
	}
}

func (m *Monitor) walkDir(dir string) int {
	info, err := os.Stat(dir)
	if err != nil {
		slog.Warn("monitor directory unavailable", "dir", dir, "error", err)
		return 0
	}
	if !info.IsDir() {
		slog.Warn("monitor path is not a directory", "dir", dir)
		return 0
	}

	var count int
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if isHidden(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		if m.shouldSkip(d) {
			return nil
		}

		fileInfo, err := d.Info()
		if err != nil {
			return nil
		}

		existing, err := m.database.GetByPath(path)
		if err != nil {
			_, _ = m.database.UpsertFile(path, fileInfo.Size(), fileInfo.ModTime())
			count++
			return nil
		}

		switch existing.Status {
		case db.StatusSuccess:
			if existing.FileSize == fileInfo.Size() && sameTime(existing.ModTime, fileInfo.ModTime()) {
				return nil
			}
			_, _ = m.database.UpsertFile(path, fileInfo.Size(), fileInfo.ModTime())
			_ = m.database.UpdateStatus(existing.ID, db.StatusPending, nil, nil)
			count++

		case db.StatusFailed:
			if existing.FileSize != fileInfo.Size() || !sameTime(existing.ModTime, fileInfo.ModTime()) {
				_, _ = m.database.UpsertFile(path, fileInfo.Size(), fileInfo.ModTime())
				_ = m.database.UpdateStatus(existing.ID, db.StatusPending, nil, nil)
				count++
			}

		case db.StatusPending, db.StatusUploading:
			if existing.Status == db.StatusUploading && time.Since(existing.LastCheckedAt) > 10*time.Minute {
				_ = m.database.UpdateStatus(existing.ID, db.StatusPending, nil, nil)
				count++
			}
			return nil

		default:
		}

		return nil
	})
	return count
}

func (m *Monitor) shouldSkip(d os.DirEntry) bool {
	name := d.Name()

	if isHidden(name) {
		return true
	}

	switch name {
	case "Thumbs.db", ".DS_Store", "desktop.ini":
		return true
	}

	ext := strings.ToLower(filepath.Ext(name))
	if !m.extensions[ext] {
		return true
	}

	return false
}

func isHidden(name string) bool {
	return strings.HasPrefix(name, ".")
}

func sameTime(a, b time.Time) bool {
	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}
