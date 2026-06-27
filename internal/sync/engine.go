package sync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mainLink0435/pushpixel/internal/config"
	"github.com/mainLink0435/pushpixel/internal/db"
	"github.com/mainLink0435/pushpixel/internal/webui"
)

const (
	defaultFlushInterval      = 5 * time.Second
	defaultStorageFullCheck   = 30 * time.Second
	defaultStatusUpdateInt    = 15 * time.Second
)

type StatusPusher interface {
	SetStatus(webui.StatusResponse)
}

type uploadJob struct {
	Path     string
	DBFileID int64
	FileSize int64
}

type uploadResult struct {
	Token *UploadToken
	Job   uploadJob
}

type Engine struct {
	cfg                config.Config
	database           *db.DB
	uploader           Uploader
	webui              StatusPusher
	authed             func() bool
	paused             atomic.Bool

	uploadCh           chan uploadJob
	createCh           chan uploadResult

	flushInterval      time.Duration
	storageCheckInt    time.Duration
	statusUpdateInt    time.Duration
	pauseCheckInt      time.Duration

	dbMu               sync.Mutex
}

func NewEngine(cfg config.Config, database *db.DB, uploader Uploader, ws StatusPusher, authed func() bool) *Engine {
	return &Engine{
		cfg:              cfg,
		database:         database,
		uploader:         uploader,
		webui:            ws,
		authed:           authed,
		uploadCh:         make(chan uploadJob, 100),
		createCh:         make(chan uploadResult, 100),
		flushInterval:    defaultFlushInterval,
		storageCheckInt:  defaultStorageFullCheck,
		statusUpdateInt:  defaultStatusUpdateInt,
		pauseCheckInt:    2 * time.Second,
	}
}

func (e *Engine) Run(ctx context.Context, fileCh <-chan string) error {
	var wg sync.WaitGroup

	for i := 0; i < e.cfg.Upload.MaxConcurrent; i++ {
		wg.Add(1)
		go e.uploadWorker(ctx, &wg)
	}

	wg.Add(1)
	go e.createWorker(ctx, &wg)

	wg.Add(1)
	go e.statusUpdater(ctx, &wg)

	go e.storageFullChecker(ctx)

	e.pushStatus()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()

		case path, ok := <-fileCh:
			if !ok {
				close(e.uploadCh)
				close(e.createCh)
				wg.Wait()
				return nil
			}

			if err := e.handleFile(ctx, path); err != nil {
				slog.Error("handle file", "path", path, "error", err)
			}
		}
	}
}

func (e *Engine) IsPaused() bool {
	return e.paused.Load()
}

func (e *Engine) handleFile(ctx context.Context, path string) error {
	if !e.authed() {
		slog.Debug("skipping file — not authenticated", "path", path)
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	existing, dbErr := e.database.GetByPath(path)
	if dbErr == nil && existing.Status == db.StatusSuccess &&
		existing.FileSize == info.Size() && sameTime(existing.ModTime, info.ModTime()) {
		slog.Debug("already uploaded, skipping", "path", path)
		return nil
	}

	if dbErr == nil && existing.RetryCount >= e.cfg.Retry.MaxAttempts &&
		(existing.Status == db.StatusUploading || existing.Status == db.StatusPending) {
		slog.Warn("max auto-retries reached, permanently failed", "path", path, "retries", existing.RetryCount)
		e.dbMu.Lock()
		errMsg := "max retries exceeded"
		_ = e.database.UpdateStatus(existing.ID, db.StatusFailed, nil, &errMsg)
		e.dbMu.Unlock()
		return nil
	}

	e.dbMu.Lock()
	record, err := e.database.UpsertFile(path, info.Size(), info.ModTime())
	e.dbMu.Unlock()
	if err != nil {
		return err
	}

	e.dbMu.Lock()
	err = e.database.UpdateStatus(record.ID, db.StatusUploading, nil, nil)
	e.dbMu.Unlock()
	if err != nil {
		return err
	}

	job := uploadJob{
		Path:     path,
		DBFileID: record.ID,
		FileSize: info.Size(),
	}

	e.waitForPause(ctx)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case e.uploadCh <- job:
		return nil
	}
}

func (e *Engine) uploadWorker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-e.uploadCh:
			if !ok {
				return
			}

			e.waitForPause(ctx)

			slog.Debug("uploading bytes", "path", job.Path, "size", job.FileSize)

			token, err := e.uploader.UploadFile(ctx, job.Path)
			if err != nil {
				var perr PermanentError
				if errors.As(err, &perr) {
					slog.Error("byte upload permanently failed", "path", job.Path, "error", err)
					e.dbMu.Lock()
					_ = e.database.UpdateStatus(job.DBFileID, db.StatusFailed, nil, strPtr(err.Error()))
					e.dbMu.Unlock()
				} else {
					slog.Warn("byte upload transient error, will retry", "path", job.Path, "error", err)
					e.dbMu.Lock()
					_ = e.database.IncrementRetryCount(job.DBFileID)
					_ = e.database.UpdateStatus(job.DBFileID, db.StatusPending, nil, nil)
					e.dbMu.Unlock()
				}
				continue
			}

			slog.Debug("byte upload complete", "path", job.Path)

			select {
			case <-ctx.Done():
				return
			case e.createCh <- uploadResult{Token: token, Job: job}:
			}
		}
	}
}

func (e *Engine) createWorker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	var batch []*UploadToken
	var pendingJobs []uploadJob

	ticker := time.NewTicker(e.flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		e.waitForPause(ctx)

		results, err := e.uploader.BatchCreate(ctx, batch)
		if err != nil {
			errStr := err.Error()

			if errStr == "storage full" || errStr == "rate limited" {
				e.paused.Store(true)
				slog.Warn("storage full or rate limited, pausing uploads")
				return
			}

			var perr PermanentError
			isPermanent := errors.As(err, &perr)
			if isPermanent {
				slog.Error("batch create permanently failed", "error", err)
			} else {
				slog.Warn("batch create transient error, will retry", "error", err)
			}
			e.dbMu.Lock()
			newStatus := db.StatusPending
			if isPermanent {
				newStatus = db.StatusFailed
			}
			for _, job := range pendingJobs {
				if !isPermanent {
					_ = e.database.IncrementRetryCount(job.DBFileID)
				}
				_ = e.database.UpdateStatus(job.DBFileID, newStatus, nil, &errStr)
			}
			e.dbMu.Unlock()
			batch = batch[:0]
			pendingJobs = pendingJobs[:0]
			return
		}

		e.dbMu.Lock()
		for i, result := range results {
			job := pendingJobs[i]
			if result.Status == "success" {
				_ = e.database.UpdateStatus(job.DBFileID, db.StatusSuccess, &result.MediaItemID, nil)
				slog.Info("upload success", "path", job.Path, "media_id", result.MediaItemID)
			} else {
				_ = e.database.UpdateStatus(job.DBFileID, db.StatusFailed, nil, &result.Error)
				slog.Warn("upload failed", "path", job.Path, "error", result.Error)
			}
		}
		e.dbMu.Unlock()

		batch = batch[:0]
		pendingJobs = pendingJobs[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return

		case result, ok := <-e.createCh:
			if !ok {
				flush()
				return
			}

			batch = append(batch, result.Token)
			pendingJobs = append(pendingJobs, result.Job)

			if len(batch) >= e.cfg.Upload.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (e *Engine) statusUpdater(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(e.statusUpdateInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.pushStatus()
		}
	}
}

func (e *Engine) pushStatus() {
	e.dbMu.Lock()
	totalFiles, _ := e.database.TotalCount()
	totalUploaded, _ := e.database.CountByStatus(db.StatusSuccess)
	pending, _ := e.database.CountByStatus(db.StatusPending)
	uploading, _ := e.database.CountByStatus(db.StatusUploading)
	totalFailed, _ := e.database.CountByStatus(db.StatusFailed)
	e.dbMu.Unlock()

	e.webui.SetStatus(webui.StatusResponse{
		Authenticated:  e.authed(),
		StorageFull:    e.paused.Load(),
		TotalFiles:     totalFiles,
		Uploaded:       totalUploaded,
		Remaining:      pending + uploading,
		Failed:         totalFailed,
	})
}

func (e *Engine) storageFullChecker(ctx context.Context) {
	ticker := time.NewTicker(e.storageCheckInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.paused.Load() {
				slog.Info("storage full auto-resume check")
				e.paused.Store(false)
				slog.Info("auto-resumed after storage full backoff")
			}
		}
	}
}

func (e *Engine) waitForPause(ctx context.Context) {
	for e.paused.Load() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.pauseCheckInt):
		}
	}
}

func strPtr(s string) *string {
	return &s
}

func sameTime(a, b time.Time) bool {
	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}
