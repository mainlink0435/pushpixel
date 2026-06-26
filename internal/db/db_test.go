package db

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func setupDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpen_CreatesTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	f, err := db.GetByPath("/nonexistent")
	if err == nil || f != nil {
		t.Fatal("expected error for missing file")
	}
}

func TestUpsertFile_New(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	f, err := db.UpsertFile("/path/to/photo.jpg", 1024, now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if f.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if f.Status != StatusPending {
		t.Errorf("expected pending, got %s", f.Status)
	}
	if f.FileSize != 1024 {
		t.Errorf("expected size 1024, got %d", f.FileSize)
	}
}

func TestUpsertFile_UpdateExisting(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	f1, err := db.UpsertFile("/path/to/photo.jpg", 1024, now)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updatedSize := int64(2048)
	f2, err := db.UpsertFile("/path/to/photo.jpg", updatedSize, now)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if f1.ID != f2.ID {
		t.Errorf("expected same ID %d, got %d", f1.ID, f2.ID)
	}
	if f2.FileSize != updatedSize {
		t.Errorf("expected updated size %d, got %d", updatedSize, f2.FileSize)
	}
}

func TestGetByPath_Found(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	original, err := db.UpsertFile("/path/to/photo.jpg", 1024, now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	found, err := db.GetByPath("/path/to/photo.jpg")
	if err != nil {
		t.Fatalf("get by path: %v", err)
	}
	if found.ID != original.ID {
		t.Errorf("expected ID %d, got %d", original.ID, found.ID)
	}
}

func TestGetByPath_NotFound(t *testing.T) {
	db := setupDB(t)
	_, err := db.GetByPath("/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestListByStatus(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	paths := []string{"/a.jpg", "/b.jpg", "/c.jpg"}
	for _, p := range paths {
		f, err := db.UpsertFile(p, 100, now)
		if err != nil {
			t.Fatalf("upsert %s: %v", p, err)
		}
		if err := db.UpdateStatus(f.ID, StatusSuccess, strPtr("media-"+f.AbsolutePath), nil); err != nil {
			t.Fatalf("update status: %v", err)
		}
	}

	// Add one pending
	pending, _ := db.UpsertFile("/d.jpg", 100, now)

	files, err := db.ListByStatus(StatusSuccess)
	if err != nil {
		t.Fatalf("list by status: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("expected 3 success files, got %d", len(files))
	}

	pendingFiles, err := db.ListByStatus(StatusPending)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pendingFiles) != 1 || pendingFiles[0].ID != pending.ID {
		t.Errorf("expected 1 pending file")
	}
}

func TestUpdateStatus_Success(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	f, err := db.UpsertFile("/photo.jpg", 100, now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	mediaID := "google-media-123"
	if err := db.UpdateStatus(f.ID, StatusSuccess, &mediaID, nil); err != nil {
		t.Fatalf("update status: %v", err)
	}

	updated, err := db.GetByID(f.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if updated.Status != StatusSuccess {
		t.Errorf("expected success, got %s", updated.Status)
	}
	if updated.GoogleMediaID == nil || *updated.GoogleMediaID != mediaID {
		t.Errorf("expected media ID %s", mediaID)
	}
	if updated.UploadedAt == nil {
		t.Fatal("expected uploaded_at to be set")
	}
}

func TestUpdateStatus_Failed(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	f, err := db.UpsertFile("/photo.jpg", 100, now)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	errMsg := "file too large"
	if err := db.UpdateStatus(f.ID, StatusFailed, nil, &errMsg); err != nil {
		t.Fatalf("update status: %v", err)
	}

	updated, err := db.GetByID(f.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if updated.Status != StatusFailed {
		t.Errorf("expected failed, got %s", updated.Status)
	}
	if updated.ErrorMessage == nil || *updated.ErrorMessage != errMsg {
		t.Errorf("expected error message %s", errMsg)
	}
}

func TestConcurrentOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	defer db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()

	_, err = db1.UpsertFile("/test.jpg", 100, time.Now())
	if err != nil {
		t.Fatalf("db1 upsert: %v", err)
	}
}

func TestCountByStatus(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	for i := 0; i < 5; i++ {
		path := filepath.Join("/", fmt.Sprintf("success_%d.jpg", i))
		f, err := db.UpsertFile(path, 100, now)
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := db.UpdateStatus(f.ID, StatusSuccess, strPtr("mid-"+path), nil); err != nil {
			t.Fatalf("update: %v", err)
		}
	}

	for i := 0; i < 3; i++ {
		path := filepath.Join("/", fmt.Sprintf("failed_%d.jpg", i))
		f, err := db.UpsertFile(path, 100, now)
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := db.UpdateStatus(f.ID, StatusFailed, nil, strPtr("error")); err != nil {
			t.Fatalf("update: %v", err)
		}
	}

	successCount, err := db.CountByStatus(StatusSuccess)
	if err != nil {
		t.Fatalf("count success: %v", err)
	}
	if successCount != 5 {
		t.Errorf("expected 5 success, got %d", successCount)
	}

	failedCount, err := db.CountByStatus(StatusFailed)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if failedCount != 3 {
		t.Errorf("expected 3 failed, got %d", failedCount)
	}
}

func TestListByStatus_IncludesAllStatusTypes(t *testing.T) {
	db := setupDB(t)
	now := time.Now()

	statuses := []Status{StatusPending, StatusUploading, StatusSuccess, StatusFailed}
	for i, s := range statuses {
		f, err := db.UpsertFile(filepath.Join("/", string(s)+".jpg"), int64(i+1)*100, now)
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := db.UpdateStatus(f.ID, s, nil, nil); err != nil {
			t.Fatalf("update to %s: %v", s, err)
		}
	}

	for _, s := range statuses {
		files, err := db.ListByStatus(s)
		if err != nil {
			t.Fatalf("list %s: %v", s, err)
		}
		if len(files) != 1 {
			t.Errorf("expected 1 file with status %s, got %d", s, len(files))
		}
	}
}

func strPtr(s string) *string {
	return &s
}
