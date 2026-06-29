package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mainLink0435/pushpixel/internal/db"
)

func setupTest(t *testing.T) (*db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.OpenTest(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database, dir
}

func writeFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("test-data"), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestNewFileAdded(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, "photo.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	select {
	case path := <-ch:
		if !strings.HasSuffix(path, "photo.jpg") {
			t.Errorf("expected photo.jpg, got %s", path)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for file")
	}
}

func TestAlreadySuccessFile(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	path := writeFile(t, dir, "photo.jpg")
	info, _ := os.Stat(path)

	record, err := database.UpsertFile(path, info.Size(), info.ModTime())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	mediaID := "existing-media-id"
	if err := database.UpdateStatus(record.ID, db.StatusSuccess, &mediaID, nil); err != nil {
		t.Fatalf("update status: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)
	_ = ch

	time.Sleep(200 * time.Millisecond)
	paths := drain(ch, 200*time.Millisecond)
	if len(paths) > 0 {
		t.Errorf("expected no files for already-success, got %v", paths)
	}
}

func TestHiddenFilesSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, ".hidden.jpg")
	writeFile(t, dir, "visible.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 1 {
		t.Errorf("expected 1 visible file, got %d: %v", len(paths), paths)
	}
}

func TestSystemFilesSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, "Thumbs.db")
	writeFile(t, dir, ".DS_Store")
	writeFile(t, dir, "desktop.ini")
	writeFile(t, dir, "real.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "real.jpg") {
		t.Errorf("expected real.jpg only, got %v", paths)
	}
}

func TestWrongExtensionSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, "doc.pdf")
	writeFile(t, dir, "video.mp4")
	writeFile(t, dir, "image.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "image.jpg") {
		t.Errorf("expected image.jpg only, got %v", paths)
	}
}

func TestHiddenDirSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	hiddenDir := filepath.Join(dir, ".hidden")
	if err := os.MkdirAll(hiddenDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, hiddenDir, "in_hidden.jpg")
	writeFile(t, dir, "visible.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "visible.jpg") {
		t.Errorf("expected visible.jpg only, got %v", paths)
	}
}

func TestFailedFileModifiedRequeued(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	path := writeFile(t, dir, "photo.jpg")
	info, _ := os.Stat(path)

	record, err := database.UpsertFile(path, info.Size(), info.ModTime())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	errMsg := "permanent error"
	if err := database.UpdateStatus(record.ID, db.StatusFailed, nil, &errMsg); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := os.WriteFile(path, []byte("modified-data"), 0644); err != nil {
		t.Fatalf("modify: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 1 {
		t.Errorf("expected modified failed file to be requeued, got %v", paths)
	}

	updated, _ := database.GetByPath(path)
	if updated.Status != db.StatusPending {
		t.Errorf("expected status pending, got %s", updated.Status)
	}
}

func TestMultipleDirectories(t *testing.T) {
	database, dir := setupTest(t)
	dir1 := filepath.Join(dir, "a")
	dir2 := filepath.Join(dir, "b")
	os.MkdirAll(dir1, 0755)
	os.MkdirAll(dir2, 0755)

	m := New([]string{dir1, dir2}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir1, "from_a.jpg")
	writeFile(t, dir2, "from_b.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 2 {
		t.Errorf("expected 2 files, got %v", paths)
	}
}

func TestNonExistentDirSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{"/nonexistent/path"}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, "photo.jpg")
	database.UpsertFile(filepath.Join(dir, "photo.jpg"), 10, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = m.Start(ctx)
	time.Sleep(200 * time.Millisecond)
}

func TestUnchangedFailedFileNotRequeued(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	path := writeFile(t, dir, "photo.jpg")
	info, _ := os.Stat(path)

	record, err := database.UpsertFile(path, info.Size(), info.ModTime())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	errMsg := "permanent error"
	if err := database.UpdateStatus(record.ID, db.StatusFailed, nil, &errMsg); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Start(ctx)

	paths := drain(ch, time.Second)
	if len(paths) != 0 {
		t.Errorf("expected unchanged failed file not requeued, got %v", paths)
	}
}

func drain(ch <-chan string, timeout time.Duration) []string {
	var paths []string
	timer := time.After(timeout)
	for {
		select {
		case p, ok := <-ch:
			if !ok {
				return paths
			}
			paths = append(paths, p)
		case <-timer:
			return paths
		}
	}
}
