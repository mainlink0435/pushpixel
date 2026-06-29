package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mainLink0435/pushpixel/internal/config"
	"github.com/mainLink0435/pushpixel/internal/db"
	"github.com/mainLink0435/pushpixel/internal/webui"
)

type mockUploaderImpl struct {
	uploadFunc   func(ctx context.Context, path string) (*UploadToken, error)
	batchFunc    func(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error)
}

func (m *mockUploaderImpl) UploadFile(ctx context.Context, path string) (*UploadToken, error) {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, path)
	}
	return &UploadToken{Token: "mock-token", Path: path, Name: filepath.Base(path)}, nil
}

func (m *mockUploaderImpl) BatchCreate(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error) {
	if m.batchFunc != nil {
		return m.batchFunc(ctx, tokens)
	}
	var results []BatchResult
	for _, t := range tokens {
		results = append(results, BatchResult{
			Token:       t.Token,
			Status:      "success",
			MediaItemID: "media-" + t.Token,
		})
	}
	return results, nil
}

type mockStatusPusher struct {
	mu         sync.RWMutex
	lastStatus webui.StatusResponse
}

func (m *mockStatusPusher) SetStatus(s webui.StatusResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastStatus = s
}

func (m *mockStatusPusher) GetStatus() webui.StatusResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastStatus
}

func setupEngineTest(t *testing.T, batchSize int) (*Engine, *db.DB, chan string, *mockUploaderImpl, *mockStatusPusher) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.OpenTest(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	mockUp := &mockUploaderImpl{}
	mockWS := &mockStatusPusher{}
	cfg := config.Config{}
	cfg.Upload.BatchSize = batchSize
	cfg.Upload.MaxConcurrent = 1
	cfg.Retry.MaxAttempts = 3

	e := NewEngine(cfg, database, mockUp, mockWS, func() bool { return true })
	e.flushInterval = 50 * time.Millisecond
	e.storageCheckInt = 5 * time.Minute
	e.statusUpdateInt = 50 * time.Millisecond
	e.pauseCheckInt = 50 * time.Millisecond

	fileCh := make(chan string, 10)

	return e, database, fileCh, mockUp, mockWS
}

func writeTestFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "test.jpg")
	if err := os.WriteFile(path, []byte("test-data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func awaitStatus(t *testing.T, d *db.DB, path string, target db.Status) *db.TrackedFile {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		record, err := d.GetByPath(path)
		if err == nil && record.Status == target {
			return record
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("timed out waiting for %s, last error: %v", target, err)
			}
			record, _ := d.GetByPath(path)
			t.Fatalf("timed out waiting for %s, got status=%s", target, record.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestEngine_SuccessUpload(t *testing.T) {
	e, database, fileCh, _, _ := setupEngineTest(t, 5)
	dir := t.TempDir()
	path := writeTestFile(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- path

	record := awaitStatus(t, database, path, db.StatusSuccess)

	if record.GoogleMediaID == nil || *record.GoogleMediaID == "" {
		t.Error("expected google media ID")
	}
}

func TestEngine_TransientUploadFailure(t *testing.T) {
	e, database, fileCh, mockUp, _ := setupEngineTest(t, 5)
	dir := t.TempDir()
	path := writeTestFile(t, dir)

	mockUp.uploadFunc = func(ctx context.Context, path string) (*UploadToken, error) {
		return nil, errors.New("transient error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- path

	awaitStatus(t, database, path, db.StatusPending)
}

func TestEngine_PermanentUploadFailure(t *testing.T) {
	e, database, fileCh, mockUp, _ := setupEngineTest(t, 5)
	dir := t.TempDir()
	path := writeTestFile(t, dir)

	mockUp.uploadFunc = func(ctx context.Context, path string) (*UploadToken, error) {
		return nil, PermanentError{Message: "file too large"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- path

	record := awaitStatus(t, database, path, db.StatusFailed)

	if record.ErrorMessage == nil || *record.ErrorMessage != "file too large" {
		t.Errorf("expected error message 'file too large', got %v", record.ErrorMessage)
	}
}

func TestEngine_BatchCreateTransientFailure(t *testing.T) {
	e, database, fileCh, mockUp, _ := setupEngineTest(t, 1)
	dir := t.TempDir()
	path := writeTestFile(t, dir)

	mockUp.uploadFunc = func(ctx context.Context, path string) (*UploadToken, error) {
		return &UploadToken{Token: "tok", Path: path, Name: "test.jpg"}, nil
	}
	mockUp.batchFunc = func(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error) {
		return nil, errors.New("transient batch error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- path

	awaitStatus(t, database, path, db.StatusPending)
}

func TestEngine_BatchCreatePermanentFailure(t *testing.T) {
	e, database, fileCh, mockUp, _ := setupEngineTest(t, 1)
	dir := t.TempDir()
	path := writeTestFile(t, dir)

	mockUp.uploadFunc = func(ctx context.Context, path string) (*UploadToken, error) {
		return &UploadToken{Token: "tok", Path: path, Name: "test.jpg"}, nil
	}
	mockUp.batchFunc = func(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error) {
		return nil, PermanentError{Message: "invalid request"}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- path

	record := awaitStatus(t, database, path, db.StatusFailed)

	if record.ErrorMessage == nil || *record.ErrorMessage != "invalid request" {
		t.Errorf("expected error message 'invalid request', got %v", record.ErrorMessage)
	}
}

func TestEngine_StorageFullPause(t *testing.T) {
	e, _, fileCh, mockUp, _ := setupEngineTest(t, 1)
	dir := t.TempDir()

	mockUp.uploadFunc = func(ctx context.Context, path string) (*UploadToken, error) {
		return &UploadToken{Token: "tok", Path: path, Name: "test.jpg"}, nil
	}
	mockUp.batchFunc = func(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error) {
		return nil, errors.New("storage full")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- writeTestFile(t, dir)

	time.Sleep(300 * time.Millisecond)

	if !e.IsPaused() {
		t.Fatal("expected paused after storage full")
	}
}

func TestEngine_ResumeAfterStorageFull(t *testing.T) {
	e, database, fileCh, mockUp, _ := setupEngineTest(t, 1)
	dir := t.TempDir()

	callCount := 0
	batchFunc := func(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error) {
		callCount++
		if callCount == 1 {
			return nil, errors.New("storage full")
		}
		return []BatchResult{{
			Token: tokens[0].Token, Status: "success",
			MediaItemID: "media-" + tokens[0].Token,
		}}, nil
	}
	mockUp.batchFunc = batchFunc

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)

	fileCh <- writeTestFile(t, dir)
	time.Sleep(300 * time.Millisecond)

	if !e.IsPaused() {
		t.Fatal("expected paused after storage full")
	}

	e.paused.Store(false)
	time.Sleep(100 * time.Millisecond)

	dir2 := t.TempDir()
	p2 := writeTestFile(t, dir2)
	fileCh <- p2

	awaitStatus(t, database, p2, db.StatusSuccess)
}

func TestEngine_ContextCancellation(t *testing.T) {
	e, _, fileCh, _, _ := setupEngineTest(t, 5)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- e.Run(ctx, fileCh)
	}()

	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("expected Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("engine did not stop")
	}
}

func TestEngine_FileChannelClosure(t *testing.T) {
	e, _, fileCh, _, _ := setupEngineTest(t, 5)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- e.Run(ctx, fileCh)
	}()

	close(fileCh)
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Errorf("expected nil or Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("engine did not stop after file channel close")
	}
}

func TestEngine_MultipleFiles(t *testing.T) {
	e, database, fileCh, _, _ := setupEngineTest(t, 1)
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)

	paths := []string{}
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, fmt.Sprintf("photo_%d.jpg", i))
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		paths = append(paths, p)
		fileCh <- p
	}

	for _, path := range paths {
		awaitStatus(t, database, path, db.StatusSuccess)
	}
}

func TestEngine_StatusUpdate(t *testing.T) {
	e, _, fileCh, _, mockWS := setupEngineTest(t, 5)
	dir := t.TempDir()
	path := writeTestFile(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go e.Run(ctx, fileCh)
	fileCh <- path

	time.Sleep(800 * time.Millisecond)

	status := mockWS.GetStatus()
	if !status.Authenticated {
		t.Error("expected authenticated")
	}
	if status.Uploaded < 1 {
		t.Errorf("expected >=1 uploaded, got %d", status.Uploaded)
	}
}

func TestEngine_NotAuthenticated(t *testing.T) {
	dir := t.TempDir()
	database, err := db.OpenTest(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	defer database.Close()

	mockWS := &mockStatusPusher{}
	cfg := config.Config{}
	cfg.Upload.MaxConcurrent = 1

	e := NewEngine(cfg, database, &mockUploaderImpl{}, mockWS, func() bool { return false })
	e.statusUpdateInt = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fileCh := make(chan string)
	go e.Run(ctx, fileCh)

	time.Sleep(200 * time.Millisecond)

	status := mockWS.GetStatus()
	if status.Authenticated {
		t.Error("expected not authenticated")
	}
}
