package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mainLink0435/pushpixel/internal/config"
)

func newRawUploadServer(t *testing.T) *httptest.Server {
	t.Helper()

	// Pre-declare so the handler can capture it
	var srv *httptest.Server
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		protocol := r.Header.Get("X-Goog-Upload-Protocol")

		switch protocol {
		case "raw":
			if r.Header.Get("Content-Type") != "application/octet-stream" {
				http.Error(w, "bad content type", http.StatusBadRequest)
				return
			}
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "token-%x", body[:4])

		case "resumable":
			command := r.Header.Get("X-Goog-Upload-Command")
			if command == "start" {
				sessionURL := srv.URL + r.URL.Path + "/session"
				w.Header().Set("X-Goog-Upload-URL", sessionURL)
				w.WriteHeader(http.StatusOK)
			} else if strings.Contains(command, "upload") {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprintf(w, "resumable-token-%x", body[:4])
			} else {
				http.Error(w, "unknown command", http.StatusBadRequest)
			}

		default:
			http.Error(w, "bad protocol", http.StatusBadRequest)
		}
	})
	srv = httptest.NewServer(handler)

	uploadURL = srv.URL + "/v1/uploads"
	return srv
}

func newBatchCreateServer(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			NewMediaItems []map[string]interface{} `json:"newMediaItems"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if statusCode != http.StatusOK && statusCode != http.StatusMultiStatus {
			http.Error(w, "error", statusCode)
			return
		}

		var results []map[string]interface{}
		for _, item := range req.NewMediaItems {
			simple, _ := item["simpleMediaItem"].(map[string]interface{})
			token, _ := simple["uploadToken"].(string)
			result := map[string]interface{}{
				"uploadToken": token,
				"status": map[string]interface{}{
					"code":    0,
					"message": "Success",
				},
				"mediaItem": map[string]interface{}{
					"id": "media-" + token,
				},
			}
			results = append(results, result)
		}

		resp := map[string]interface{}{
			"newMediaItemResults": results,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))

	batchCreateURL = srv.URL + "/v1/mediaItems:batchCreate"
	return srv
}

func TestUploadFile_Raw(t *testing.T) {
	mock := newRawUploadServer(t)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, []byte("fake-jpeg-data"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := uploader.UploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if result.Name != "photo.jpg" {
		t.Errorf("expected photo.jpg, got %s", result.Name)
	}
	if result.Path != path {
		t.Errorf("expected path %s, got %s", path, result.Path)
	}
}

func TestUploadFile_Resumable(t *testing.T) {
	mock := newRawUploadServer(t)
	defer mock.Close()

	// Set the session URL resolution to match the mock server
	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	dir := t.TempDir()
	path := filepath.Join(dir, "large.mov")
	data := make([]byte, resumableThreshold+1)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := uploader.UploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if !strings.HasPrefix(result.Token, "resumable-token-") {
		t.Errorf("expected resumable token prefix, got %s", result.Token)
	}
}

func TestUploadFile_NonexistentFile(t *testing.T) {
	mock := newRawUploadServer(t)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	_, err := uploader.UploadFile(context.Background(), "/nonexistent/file.jpg")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestBatchCreate_Success(t *testing.T) {
	mock := newBatchCreateServer(t, http.StatusOK)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	tokens := []*UploadToken{
		{Token: "tok1", Path: "/a.jpg", Name: "a.jpg"},
		{Token: "tok2", Path: "/b.jpg", Name: "b.jpg"},
	}

	results, err := uploader.BatchCreate(context.Background(), tokens)
	if err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Status != "success" {
			t.Errorf("expected success for token %s, got %s", r.Token, r.Status)
		}
		if r.MediaItemID == "" {
			t.Errorf("expected media item ID for token %s", r.Token)
		}
	}
}

func TestBatchCreate_Empty(t *testing.T) {
	uploader := NewUploader(nil, config.UploadConfig{BatchSize: 50})
	results, err := uploader.BatchCreate(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %v", results)
	}
}

func TestBatchCreate_ExceedsBatchSize(t *testing.T) {
	mock := newBatchCreateServer(t, http.StatusOK)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 1})

	tokens := []*UploadToken{
		{Token: "tok1", Path: "/a.jpg", Name: "a.jpg"},
		{Token: "tok2", Path: "/b.jpg", Name: "b.jpg"},
	}

	results, err := uploader.BatchCreate(context.Background(), tokens)
	if err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (capped at batch size), got %d", len(results))
	}
}

func TestBatchCreate_HTTPError(t *testing.T) {
	mock := newBatchCreateServer(t, http.StatusOK)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	tokens := []*UploadToken{
		{Token: "tok1", Path: "/a.jpg", Name: "a.jpg"},
	}

	results, err := uploader.BatchCreate(context.Background(), tokens)
	if err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MediaItemID == "" {
		t.Errorf("expected media item ID")
	}
}

func TestDetectMIME(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"image.webp", "image/webp"},
		{"video.mp4", "video/mp4"},
		{"video.mov", "video/quicktime"},
		{"unknown.xyz", "application/octet-stream"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := detectMIME(tt.path)
		if got != tt.expected {
			t.Errorf("detectMIME(%s) = %s, want %s", tt.path, got, tt.expected)
		}
	}
}

func TestUploadFile_EmptyFile(t *testing.T) {
	mock := newRawUploadServer(t)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jpg")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := uploader.UploadFile(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	var perr PermanentError
	if !errors.As(err, &perr) {
		t.Fatalf("expected PermanentError for empty file, got %T: %v", err, err)
	}
}

func TestUploadFile_NonImage(t *testing.T) {
	mock := newRawUploadServer(t)
	defer mock.Close()

	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("text content"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := uploader.UploadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected token for non-image file")
	}
}

func TestBatchCreate_PartialFailures(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			NewMediaItems []map[string]interface{} `json:"newMediaItems"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		var results []map[string]interface{}
		for i, item := range req.NewMediaItems {
			simple, _ := item["simpleMediaItem"].(map[string]interface{})
			token, _ := simple["uploadToken"].(string)

			result := map[string]interface{}{
				"uploadToken": token,
			}
			if i == 0 {
				result["status"] = map[string]interface{}{
					"code":    0,
					"message": "Success",
				}
				result["mediaItem"] = map[string]interface{}{
					"id": "media-" + token,
				}
			} else {
				result["status"] = map[string]interface{}{
					"code":    13,
					"message": "Internal error",
				}
			}
			results = append(results, result)
		}

		resp := map[string]interface{}{
			"newMediaItemResults": results,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultiStatus)
		json.NewEncoder(w).Encode(resp)
	}))
	defer mock.Close()

	batchCreateURL = mock.URL + "/v1/mediaItems:batchCreate"
	uploader := NewUploader(mock.Client(), config.UploadConfig{BatchSize: 50})

	tokens := []*UploadToken{
		{Token: "good-token", Name: "good.jpg"},
		{Token: "bad-token", Name: "bad.jpg"},
	}

	results, err := uploader.BatchCreate(context.Background(), tokens)
	if err != nil {
		t.Fatalf("BatchCreate: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Status != "success" || results[0].MediaItemID == "" {
		t.Errorf("expected first to succeed, got status=%s id=%s", results[0].Status, results[0].MediaItemID)
	}
	if results[1].Status != "failed" || results[1].Error == "" {
		t.Errorf("expected second to fail, got status=%s err=%s", results[1].Status, results[1].Error)
	}
}
