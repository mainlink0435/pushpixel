package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mainLink0435/pushpixel/internal/config"
)

var (
	uploadURL     = "https://photoslibrary.googleapis.com/v1/uploads"
	batchCreateURL = "https://photoslibrary.googleapis.com/v1/mediaItems:batchCreate"
)

const resumableThreshold = 50 * 1024 * 1024

type Uploader interface {
	UploadFile(ctx context.Context, path string) (*UploadToken, error)
	BatchCreate(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error)
}

type PermanentError struct {
	Message string
}

func (e PermanentError) Error() string {
	return e.Message
}

type UploadToken struct {
	Token string
	Path  string
	Name  string
}

type BatchResult struct {
	Token       string
	MediaItemID string
	Status      string
	Error       string
}

type mediaUploader struct {
	client   *http.Client
	maxBatch int
}

func NewUploader(client *http.Client, cfg config.UploadConfig) Uploader {
	return &mediaUploader{
		client:   client,
		maxBatch: cfg.BatchSize,
	}
}

func (u *mediaUploader) UploadFile(ctx context.Context, path string) (*UploadToken, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	if info.Size() == 0 {
		return nil, PermanentError{Message: "file is empty (0 bytes)"}
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	mimeType := detectMIME(path)
	fileSize := info.Size()

	var token string

	if fileSize >= resumableThreshold {
		token, err = u.uploadResumable(ctx, file, fileSize, mimeType)
	} else {
		token, err = u.uploadRaw(ctx, file, mimeType)
	}

	if err != nil {
		return nil, err
	}

	return &UploadToken{
		Token: token,
		Path:  path,
		Name:  filepath.Base(path),
	}, nil
}

func (u *mediaUploader) uploadRaw(ctx context.Context, file *os.File, mimeType string) (string, error) {
	body, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Goog-Upload-Content-Type", mimeType)
	req.Header.Set("X-Goog-Upload-Protocol", "raw")

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("upload failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
		if isPermanentHTTP(resp.StatusCode) {
			return "", PermanentError{Message: errMsg}
		}
		return "", fmt.Errorf("%s", errMsg)
	}

	tokenBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("empty upload token")
	}

	return token, nil
}

func (u *mediaUploader) uploadResumable(ctx context.Context, file *os.File, fileSize int64, mimeType string) (string, error) {
	sessionURL, err := u.startResumableSession(ctx, fileSize, mimeType)
	if err != nil {
		return "", fmt.Errorf("start resumable: %w", err)
	}

	return u.finalizeResumable(ctx, sessionURL, file, fileSize, mimeType)
}

func (u *mediaUploader) startResumableSession(ctx context.Context, fileSize int64, mimeType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create start request: %w", err)
	}

	req.Header.Set("X-Goog-Upload-Protocol", "resumable")
	req.Header.Set("X-Goog-Upload-Command", "start")
	req.Header.Set("X-Goog-Upload-Content-Type", mimeType)
	req.Header.Set("Content-Length", "0")

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("start session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("start resumable failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
		if isPermanentHTTP(resp.StatusCode) {
			return "", PermanentError{Message: errMsg}
		}
		return "", fmt.Errorf("%s", errMsg)
	}

	sessionURL := resp.Header.Get("X-Goog-Upload-URL")
	if sessionURL == "" {
		return "", fmt.Errorf("no X-Goog-Upload-URL in response")
	}

	return sessionURL, nil
}

func (u *mediaUploader) finalizeResumable(ctx context.Context, sessionURL string, file *os.File, fileSize int64, mimeType string) (string, error) {
	body, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sessionURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}

	req.Header.Set("Content-Length", fmt.Sprintf("%d", fileSize))
	req.Header.Set("X-Goog-Upload-Protocol", "resumable")
	req.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	req.Header.Set("X-Goog-Upload-Offset", "0")
	req.Header.Set("X-Goog-Upload-Content-Type", mimeType)

	resp, err := u.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("resumable upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("resumable upload failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
		if isPermanentHTTP(resp.StatusCode) {
			return "", PermanentError{Message: errMsg}
		}
		return "", fmt.Errorf("%s", errMsg)
	}

	tokenBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("empty upload token from resumable")
	}

	return token, nil
}

func (u *mediaUploader) BatchCreate(ctx context.Context, tokens []*UploadToken) ([]BatchResult, error) {
	if len(tokens) == 0 {
		return nil, nil
	}

	if len(tokens) > u.maxBatch {
		tokens = tokens[:u.maxBatch]
	}

	var newMediaItems []map[string]interface{}
	for _, t := range tokens {
		newMediaItems = append(newMediaItems, map[string]interface{}{
			"simpleMediaItem": map[string]string{
				"uploadToken": t.Token,
				"fileName":    t.Name,
			},
		})
	}

	body := map[string]interface{}{
		"newMediaItems": newMediaItems,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal batch request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, batchCreateURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create batch request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("batch create request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read batch response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited")
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
		var errBody struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		if json.Unmarshal(respBytes, &errBody) == nil && errBody.Error.Status == "QUOTA_EXCEEDED" {
			return nil, fmt.Errorf("storage full")
		}
		return nil, fmt.Errorf("batch create failed (HTTP %d): %s", resp.StatusCode, string(respBytes))
	}

	var batchResp struct {
		NewMediaItemResults []struct {
			UploadToken string `json:"uploadToken"`
			Status      struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"status"`
			MediaItem *struct {
				ID string `json:"id"`
			} `json:"mediaItem"`
		} `json:"newMediaItemResults"`
	}

	if err := json.Unmarshal(respBytes, &batchResp); err != nil {
		return nil, fmt.Errorf("unmarshal batch response: %w", err)
	}

	var results []BatchResult
	for _, r := range batchResp.NewMediaItemResults {
		result := BatchResult{
			Token: r.UploadToken,
		}
		if r.Status.Code == 0 {
			result.Status = "success"
			if r.MediaItem != nil {
				result.MediaItemID = r.MediaItem.ID
			}
		} else {
			result.Status = "failed"
			result.Error = r.Status.Message
		}
		results = append(results, result)
	}

	return results, nil
}

func isPermanentHTTP(statusCode int) bool {
	if statusCode < 400 || statusCode >= 500 {
		return false
	}
	return statusCode != http.StatusTooManyRequests && statusCode != http.StatusUnauthorized && statusCode != http.StatusForbidden
}

func detectMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	default:
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			return mimeType
		}
		return "application/octet-stream"
	}
}


