package webui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/mainLink0435/pushpixel/internal/auth"
	"github.com/mainLink0435/pushpixel/internal/config"
	"github.com/mainLink0435/pushpixel/internal/db"
)

type StatusResponse struct {
	Authenticated bool   `json:"authenticated"`
	StorageFull   bool   `json:"storage_full"`
	TotalFiles    int    `json:"total_files"`
	Uploaded      int    `json:"uploaded"`
	Remaining     int    `json:"remaining"`
	Failed        int    `json:"failed"`
}

type Server struct {
	auth     *auth.Auth
	cfg      config.WebUIConfig
	database *db.DB
	mux      *http.ServeMux
	mu       sync.RWMutex
	status   StatusResponse
}

func New(a *auth.Auth, cfg config.WebUIConfig, database *db.DB) *Server {
	s := &Server{
		auth:     a,
		cfg:      cfg,
		database: database,
		mux:      http.NewServeMux(),
	}

	a.SetRedirectURL(fmt.Sprintf("http://%s:%d/oauth/callback", cfg.Host, cfg.Port))

	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/oauth/authorize", s.handleOAuthAuthorize)
	s.mux.HandleFunc("/oauth/callback", s.handleOAuthCallback)
	s.mux.HandleFunc("/api/status", s.handleAPIStatus)
	s.mux.HandleFunc("/api/retry-failed", s.handleRetryFailed)
	s.mux.HandleFunc("/api/failed", s.handleFailedFiles)
	s.mux.HandleFunc("/health", s.handleHealth)

	return s
}

const logoHTML = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 500 500" style="display:block;margin:0 auto 16px auto;max-width:200px"><defs><linearGradient id="gb" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#4285F4"/><stop offset="100%" stop-color="#1A73E8"/></linearGradient><linearGradient id="gr" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#EA4335"/><stop offset="100%" stop-color="#D93025"/></linearGradient><linearGradient id="gy" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#FBBC05"/><stop offset="100%" stop-color="#F29900"/></linearGradient><linearGradient id="gg" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#34A853"/><stop offset="100%" stop-color="#1E8E3E"/></linearGradient><linearGradient id="gd" x1="0%" y1="0%" x2="100%" y2="100%"><stop offset="0%" stop-color="#202124"/><stop offset="100%" stop-color="#3C4043"/></linearGradient><filter id="s"><feDropShadow dx="3" dy="5" stdDeviation="4" flood-color="#000" flood-opacity=".15"/></filter></defs><g stroke="#E8EAED" stroke-width="2" fill="none"><path d="M 50 450 Q 150 250 250 250 T 450 50"/><path d="M 100 500 Q 250 350 250 250 T 400 0"/><circle cx="250" cy="250" r="120" stroke-dasharray="10 15" stroke-width="4"/></g><g filter="url(#s)"><path d="M 180 350 L 180 150 A 20 20 0 0 1 200 130 L 250 130 A 70 70 0 0 1 320 200 A 70 70 0 0 1 250 270 L 220 270 L 220 350 Z" fill="url(#gb)"/><path d="M 220 170 L 250 170 A 30 30 0 0 1 280 200 A 30 30 0 0 1 250 230 L 220 230 Z" fill="#F8F9FA"/></g><g filter="url(#s)"><rect x="290" y="110" width="30" height="30" rx="6" fill="url(#gr)" transform="rotate(15 305 125)"/><rect x="330" y="70" width="20" height="20" rx="4" fill="url(#gr)" transform="rotate(25 340 80)"/><rect x="330" y="160" width="35" height="35" rx="8" fill="url(#gy)" transform="rotate(-10 347.5 177.5)"/><rect x="380" y="130" width="15" height="15" rx="3" fill="url(#gy)" transform="rotate(-20 387.5 137.5)"/><rect x="300" y="240" width="25" height="25" rx="5" fill="url(#gg)" transform="rotate(5 312.5 252.5)"/><rect x="350" y="220" width="18" height="18" rx="4" fill="url(#gg)" transform="rotate(45 359 229)"/><rect x="130" y="280" width="20" height="20" rx="4" fill="url(#gb)"/><rect x="145" y="320" width="15" height="15" rx="3" fill="url(#gb)"/></g><text x="250" y="435" font-family="'Segoe UI',Roboto,Helvetica,Arial,sans-serif" font-size="36" font-weight="800" fill="url(#gd)" text-anchor="middle" letter-spacing="2">PUSHPIXEL</text></svg>`

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	slog.Info("webui listening", "address", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if s.auth.IsAuthenticated() {
		fmt.Fprint(w, logoHTML, dashboardHTMLAuthenticated)
	} else {
		fmt.Fprint(w, logoHTML, dashboardHTMLNotAuthed)
	}
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if s.auth.IsAuthenticated() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	url, _, err := s.auth.AuthorizationURL()
	if err != nil {
		slog.Error("generate auth URL", "error", err)
		http.Error(w, "Failed to generate authorization URL", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, authRedirectHTML, url)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	if err := s.auth.Exchange(r.Context(), code, state); err != nil {
		slog.Error("auth exchange", "error", err)
		http.Error(w, "Authorization failed", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	s.mu.RLock()
	resp := s.status
	s.mu.RUnlock()

	resp.Authenticated = s.auth.IsAuthenticated()

	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleRetryFailed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	failed, err := s.database.ListByStatus(db.StatusFailed)
	if err != nil {
		slog.Error("list failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	count := 0
	for _, f := range failed {
		if err := s.database.ResetRetryCount(f.ID); err != nil {
			slog.Error("reset retry count", "id", f.ID, "error", err)
			continue
		}
		if err := s.database.UpdateStatus(f.ID, db.StatusPending, nil, nil); err != nil {
			slog.Error("reset failed file", "id", f.ID, "error", err)
			continue
		}
		count++
	}

	slog.Info("retry reset", "count", count)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"reset": count,
	})
}

func (s *Server) handleFailedFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	failed, err := s.database.ListByStatus(db.StatusFailed)
	if err != nil {
		slog.Error("list failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type failedFile struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}

	files := make([]failedFile, 0, len(failed))
	for _, f := range failed {
		errMsg := ""
		if f.ErrorMessage != nil {
			errMsg = *f.ErrorMessage
		}
		files = append(files, failedFile{Path: f.AbsolutePath, Error: errMsg})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"files": files})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

func (s *Server) SetStatus(sr StatusResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = sr
}

const dashboardHTMLNotAuthed = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PushPixel</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 640px; margin: 40px auto; padding: 0 20px; line-height: 1.6; }
h1 { color: #1a1a1a; }
.card { background: #f5f5f5; border-radius: 8px; padding: 24px; margin: 16px 0; }
.btn { display: inline-block; padding: 12px 24px; background: #4285f4; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500; }
.btn:hover { background: #3367d6; }
.status { color: #666; margin: 8px 0; }
</style>
</head>
<body>
<div class="card">
<p class="status">Not connected to Google Photos.</p>
<a class="btn" href="/oauth/authorize">Connect to Google Photos</a>
</div>
</body>
</html>`

const dashboardHTMLAuthenticated = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PushPixel</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 640px; margin: 40px auto; padding: 0 20px; line-height: 1.6; }
h1 { color: #1a1a1a; }
.card { background: #f5f5f5; border-radius: 8px; padding: 24px; margin: 16px 0; }
.stat { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #ddd; }
.stat:last-child { border-bottom: none; }
.status { color: #666; }
.status-ok { color: #34a853; font-weight: 500; }
.btn { display: inline-block; padding: 12px 24px; background: #4285f4; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500; border: none; cursor: pointer; font-size: 14px; }
.btn:hover { background: #3367d6; }
.btn-warn { background: #ea4335; }
.btn-warn:hover { background: #c5221f; }
.hidden { display: none; }
.failed-list { margin-top: 8px; font-size: 13px; color: #333; }
.failed-item { padding: 4px 0; word-break: break-all; }
.failed-item .err { color: #ea4335; }
.toggle-link { color: #1a73e8; cursor: pointer; font-size: 13px; text-decoration: underline; }
</style>
</head>
<body>
<div class="card">
<p class="status-ok" id="auth-status">Connected to Google Photos</p>
<div class="stat"><span>Total tracked</span><span id="total">0</span></div>
<div class="stat"><span>Uploaded</span><span id="uploaded">0</span></div>
<div class="stat"><span>Remaining</span><span id="remaining">0</span></div>
<div class="stat"><span>Failed</span><span id="failed">0</span> <span id="failed-toggle" class="toggle-link hidden" onclick="toggleFailed()">Show</span></div>
<div id="failed-details" class="hidden failed-list"></div>
<div class="stat"><span>Status</span><span id="storage-status">Active</span></div>
<div id="retry-section" class="hidden" style="margin-top: 16px;">
<button class="btn btn-warn" onclick="retryFailed()">Retry Failed Files</button>
<span id="retry-result" style="margin-left: 8px; color: #666;"></span>
</div>
</div>
<script>
var failedVisible = false;
async function refreshStatus() {
  try {
    const r = await fetch('/api/status');
    const d = await r.json();
    document.getElementById('total').textContent = d.total_files;
    document.getElementById('uploaded').textContent = d.uploaded;
    document.getElementById('remaining').textContent = d.remaining;
    document.getElementById('failed').textContent = d.failed;
    document.getElementById('storage-status').textContent = d.storage_full ? 'Storage Full' : 'Active';
    var ft = document.getElementById('failed-toggle');
    ft.classList.toggle('hidden', d.failed === 0);
    ft.textContent = failedVisible ? 'Hide' : 'Show (' + d.failed + ')';
    document.getElementById('retry-section').classList.toggle('hidden', d.failed === 0);
  } catch(e) { console.error(e); }
}
async function retryFailed() {
  try {
    const r = await fetch('/api/retry-failed', { method: 'POST' });
    const d = await r.json();
    document.getElementById('retry-result').textContent = 'Reset ' + d.reset + ' file(s) — next scan will retry';
    refreshStatus();
  } catch(e) { console.error(e); }
}
async function toggleFailed() {
  failedVisible = !failedVisible;
  var el = document.getElementById('failed-details');
  var ft = document.getElementById('failed-toggle');
  if (failedVisible) {
    ft.textContent = 'Hide';
    el.classList.remove('hidden');
    try {
      const r = await fetch('/api/failed');
      const d = await r.json();
      el.innerHTML = d.files.map(function(f) {
        return '<div class="failed-item">' + escapeHtml(f.path) + ' <span class="err">— ' + escapeHtml(f.error) + '</span></div>';
      }).join('');
    } catch(e) { el.innerHTML = '<div class="failed-item">Failed to load details</div>'; }
  } else {
    ft.textContent = 'Show (' + document.getElementById('failed').textContent + ')';
    el.classList.add('hidden');
  }
}
function escapeHtml(s) {
  var div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}
setInterval(refreshStatus, 5000);
refreshStatus();
</script>
</body>
</html>`

const authRedirectHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PushPixel — Authorize</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 640px; margin: 40px auto; padding: 0 20px; line-height: 1.6; }
h1 { color: #1a1a1a; }
.card { background: #f5f5f5; border-radius: 8px; padding: 24px; margin: 16px 0; }
.btn { display: inline-block; padding: 12px 24px; background: #4285f4; color: #fff; text-decoration: none; border-radius: 6px; font-weight: 500; }
.btn:hover { background: #3367d6; }
</style>
</head>
<body>
<h1>Authorize PushPixel</h1>
<div class="card">
<p>Click the button below to sign in with Google and grant PushPixel access to your Google Photos library.</p>
<a class="btn" href="%s">Sign in with Google</a>
</div>
</body>
</html>`
