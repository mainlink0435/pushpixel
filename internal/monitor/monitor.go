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

func (m *Monitor) Start(ctx context.Context) <-chan string {
	ch := make(chan string, 100)

	go m.loop(ctx, ch)

	return ch
}

func (m *Monitor) loop(ctx context.Context, ch chan<- string) {
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

func (m *Monitor) scan(ch chan<- string) {
	for _, dir := range m.dirs {
		m.walkDir(dir, ch)
	}
}

func (m *Monitor) walkDir(dir string, ch chan<- string) {
	info, err := os.Stat(dir)
	if err != nil {
		slog.Warn("monitor directory unavailable", "dir", dir, "error", err)
		return
	}
	if !info.IsDir() {
		slog.Warn("monitor path is not a directory", "dir", dir)
		return
	}

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if isHiddenDir(d.Name()) {
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
			ch <- path
			return nil
		}

		switch existing.Status {
		case db.StatusSuccess:
			if existing.FileSize == fileInfo.Size() && sameTime(existing.ModTime, fileInfo.ModTime()) {
				return nil
			}
			ch <- path

		case db.StatusFailed:
			if existing.FileSize != fileInfo.Size() || !sameTime(existing.ModTime, fileInfo.ModTime()) {
				_ = m.database.UpdateStatus(existing.ID, db.StatusPending, nil, nil)
				ch <- path
			}

			case db.StatusPending, db.StatusUploading:
			ch <- path

		default:
		}

		return nil
	})
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

func isHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".")
}

func sameTime(a, b time.Time) bool {
	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}
