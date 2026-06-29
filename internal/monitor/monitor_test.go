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

func awaitScan(ch <-chan struct{}, timeout time.Duration) bool {
	timer := time.After(timeout)
	select {
	case <-ch:
		return true
	case <-timer:
		return false
	}
}

func TestNewFileAdded(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, "photo.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	_, err := database.GetByPath(filepath.Join(dir, "photo.jpg"))
	if err != nil {
		t.Errorf("expected file in DB, got %v", err)
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
	scanCh := m.Start(ctx)
	_ = scanCh
	_ = awaitScan(scanCh, time.Second)

	updated, err := database.GetByPath(path)
	if err != nil {
		t.Fatalf("get by path: %v", err)
	}
	if updated.Status != db.StatusSuccess {
		t.Errorf("expected success, got %s", updated.Status)
	}
}

func TestHiddenFilesSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{dir}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, ".hidden.jpg")
	writeFile(t, dir, "visible.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	_, err1 := database.GetByPath(filepath.Join(dir, ".hidden.jpg"))
	if err1 == nil {
		t.Error("expected hidden file not in DB")
	}

	visible, err2 := database.GetByPath(filepath.Join(dir, "visible.jpg"))
	if err2 != nil {
		t.Error("expected visible file in DB")
	}
	_ = visible
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
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	pending, err := database.ListPendingLimit(10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || !strings.HasSuffix(pending[0].AbsolutePath, "real.jpg") {
		t.Errorf("expected 1 pending file (real.jpg), got %d", len(pending))
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
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	pending, err := database.ListPendingLimit(10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || !strings.HasSuffix(pending[0].AbsolutePath, "image.jpg") {
		t.Errorf("expected 1 pending file (image.jpg), got %d", len(pending))
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
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	_, err1 := database.GetByPath(filepath.Join(hiddenDir, "in_hidden.jpg"))
	if err1 == nil {
		t.Error("expected hidden dir file not in DB")
	}

	_, err2 := database.GetByPath(filepath.Join(dir, "visible.jpg"))
	if err2 != nil {
		t.Error("expected visible file in DB")
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
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	updated, _ := database.GetByPath(path)
	if updated.Status != db.StatusPending {
		t.Errorf("expected pending after modification, got %s", updated.Status)
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
	scanCh := m.Start(ctx)

	if !awaitScan(scanCh, time.Second) {
		t.Fatal("timed out waiting for scan")
	}

	pending, err := database.ListPendingLimit(10)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending files, got %d", len(pending))
	}
}

func TestNonExistentDirSkipped(t *testing.T) {
	database, dir := setupTest(t)
	m := New([]string{"/nonexistent/path"}, []string{".jpg"}, time.Minute, database)

	writeFile(t, dir, "photo.jpg")
	database.UpsertFile(filepath.Join(dir, "photo.jpg"), 10, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scanCh := m.Start(ctx)
	_ = awaitScan(scanCh, time.Second)
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
	scanCh := m.Start(ctx)

	_ = awaitScan(scanCh, time.Second)

	updated, err := database.GetByPath(path)
	if err != nil {
		t.Fatalf("get by path: %v", err)
	}
	if updated.Status != db.StatusFailed {
		t.Errorf("expected unchanged failed file to stay failed, got %s", updated.Status)
	}
}
