package cmd

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/utils"
)

// APIHandler handles HTTP API requests
type APIHandler struct {
	service          core.DownloadService
	port             int
	defaultOutputDir string
}

// NewAPIHandler creates a new APIHandler
func NewAPIHandler(service core.DownloadService, port int, defaultOutputDir string) *APIHandler {
	return &APIHandler{
		service:          service,
		port:             port,
		defaultOutputDir: defaultOutputDir,
	}
}

// DownloadRequest represents a download request from the browser extension
type DownloadRequest struct {
	URL                  string            `json:"url"`
	Filename             string            `json:"filename,omitempty"`
	Path                 string            `json:"path,omitempty"`
	RelativeToDefaultDir bool              `json:"relative_to_default_dir,omitempty"`
	Mirrors              []string          `json:"mirrors,omitempty"`
	SkipApproval         bool              `json:"skip_approval,omitempty"` // Extension validated request, skip TUI prompt
	Headers              map[string]string `json:"headers,omitempty"`       // Custom HTTP headers from browser (cookies, auth, etc.)
}

// Health check endpoint (Public)
func (h *APIHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"port":   h.port,
	}); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// Events endpoint (Protected)
func (h *APIHandler) Events(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get event stream
	stream, cleanup, err := h.service.StreamEvents(r.Context())
	if err != nil {
		http.Error(w, "Failed to subscribe to events", http.StatusInternalServerError)
		return
	}
	defer cleanup()

	// Flush headers immediately
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// Send events
	// Create a closer notifier
	done := r.Context().Done()

	for {
		select {
		case <-done:
			return
		case msg, ok := <-stream:
			if !ok {
				return
			}

			// Encode message to JSON
			data, err := json.Marshal(msg)
			if err != nil {
				utils.Debug("Error marshaling event: %v", err)
				continue
			}

			// Determine event type name based on struct
			// Events are in internal/engine/events package
			eventType := "unknown"
			switch msg.(type) {
			case events.DownloadStartedMsg:
				eventType = "started"
			case events.DownloadCompleteMsg:
				eventType = "complete"
			case events.DownloadErrorMsg:
				eventType = "error"
			case events.ProgressMsg:
				eventType = "progress"
			case events.DownloadPausedMsg:
				eventType = "paused"
			case events.DownloadResumedMsg:
				eventType = "resumed"
			case events.DownloadQueuedMsg:
				eventType = "queued"
			case events.DownloadRemovedMsg:
				eventType = "removed"
			case events.DownloadRequestMsg:
				eventType = "request"
			}

			// SSE Format:
			// event: <type>
			// data: <json>
			// \n
			_, _ = fmt.Fprintf(w, "event: %s\n", eventType)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// Download endpoint (Protected + Public for simple GET status if needed? No, let's protect all for now)
func (h *APIHandler) Download(w http.ResponseWriter, r *http.Request) {
	// GET request to query status
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if h.service == nil {
			http.Error(w, "Service unavailable", http.StatusInternalServerError)
			return
		}

		status, err := h.service.GetStatus(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		if err := json.NewEncoder(w).Encode(status); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Load settings once for use throughout the function
	settings, err := config.LoadSettings()
	if err != nil {
		// Fallback to defaults if loading fails (though LoadSettings handles missing file)
		settings = config.DefaultSettings()
	}

	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			utils.Debug("Error closing body: %v", err)
		}
	}()

	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	if strings.Contains(req.Path, "..") || strings.Contains(req.Filename, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	if strings.Contains(req.Filename, "/") || strings.Contains(req.Filename, "\\") {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	utils.Debug("Received download request: URL=%s, Path=%s", req.URL, req.Path)

	downloadID := uuid.New().String()
	if h.service == nil {
		http.Error(w, "Service unavailable", http.StatusInternalServerError)
		return
	}

	// Prepare output path
	outPath := req.Path
	if req.RelativeToDefaultDir && req.Path != "" {
		// Resolve relative to default download directory
		baseDir := settings.General.DefaultDownloadDir
		if baseDir == "" {
			baseDir = h.defaultOutputDir
		}
		if baseDir == "" {
			baseDir = "."
		}
		outPath = filepath.Join(baseDir, req.Path)
		if err := os.MkdirAll(outPath, 0o755); err != nil {
			http.Error(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

	} else if outPath == "" {
		if h.defaultOutputDir != "" {
			outPath = h.defaultOutputDir
			if err := os.MkdirAll(outPath, 0o755); err != nil {
				http.Error(w, "Failed to create output directory: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if settings.General.DefaultDownloadDir != "" {
				outPath = settings.General.DefaultDownloadDir
				if err := os.MkdirAll(outPath, 0o755); err != nil {
					http.Error(w, "Failed to create output directory: "+err.Error(), http.StatusInternalServerError)
					return
				}
			} else {
				outPath = "."
			}
		}
	}

	// Enforce absolute path to ensure resume works even if CWD changes
	outPath = utils.EnsureAbsPath(outPath)

	// Check settings for extension prompt and duplicates
	// Logic modified to distinguish between ACTIVE (corruption risk) and COMPLETED (overwrite safe)
	isDuplicate := false
	isActive := false

	urlForAdd := req.URL
	mirrorsForAdd := req.Mirrors
	if len(mirrorsForAdd) == 0 && strings.Contains(req.URL, ",") {
		urlForAdd, mirrorsForAdd = ParseURLArg(req.URL)
	}

	if GlobalPool.HasDownload(urlForAdd) {
		isDuplicate = true
		// Check if specifically active\
		allActive := GlobalPool.GetAll()
		for _, c := range allActive {
			if c.URL == urlForAdd {
				if c.State != nil && !c.State.Done.Load() {
					isActive = true
				}
				break
			}
		}
	}

	utils.Debug("Download request: URL=%s, SkipApproval=%v, isDuplicate=%v, isActive=%v", urlForAdd, req.SkipApproval, isDuplicate, isActive)

	// EXTENSION VETTING SHORTCUT:
	// If SkipApproval is true, we trust the extension completely.
	// The backend will auto-rename duplicate files, so no need to reject.
	if req.SkipApproval {
		// Trust extension -> Skip all prompting logic, proceed to download
		utils.Debug("Extension request: skipping all prompts, proceeding with download")
	} else {
		// Logic for prompting:
		// 1. If ExtensionPrompt is enabled
		// 2. OR if WarnOnDuplicate is enabled AND it is a duplicate
		shouldPrompt := settings.General.ExtensionPrompt || (settings.General.WarnOnDuplicate && isDuplicate)

		// Only prompt if we have a UI running (serverProgram != nil)
		if shouldPrompt {
			if serverProgram != nil {
				utils.Debug("Requesting TUI confirmation for: %s (Duplicate: %v)", req.URL, isDuplicate)

				// Send request to TUI
				if err := h.service.Publish(events.DownloadRequestMsg{
					ID:       downloadID,
					URL:      urlForAdd,
					Filename: req.Filename,
					Path:     outPath, // Use the path we resolved (default or requested)
					Mirrors:  mirrorsForAdd,
					Headers:  req.Headers,
				}); err != nil {
					http.Error(w, "Failed to notify TUI: "+err.Error(), http.StatusInternalServerError)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				// Return 202 Accepted to indicate it's pending approval
				w.WriteHeader(http.StatusAccepted)
				if err := json.NewEncoder(w).Encode(map[string]string{
					"status":  "pending_approval",
					"message": "Download request sent to TUI for confirmation",
					"id":      downloadID, // ID might change if user modifies it, but useful for tracking
				}); err != nil {
					utils.Debug("Failed to encode response: %v", err)
				}
				return
			} else {
				// Headless mode check
				if settings.General.ExtensionPrompt || (settings.General.WarnOnDuplicate && isDuplicate) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					if err := json.NewEncoder(w).Encode(map[string]string{
						"status":  "error",
						"message": "Download rejected: Duplicate download or approval required (Headless mode)",
					}); err != nil {
						utils.Debug("Failed to encode response: %v", err)
					}
					return
				}
			}
		}
	}

	// Add via service
	newID, err := h.service.Add(urlForAdd, outPath, req.Filename, mirrorsForAdd, req.Headers)
	if err != nil {
		http.Error(w, "Failed to add download: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Increment active downloads counter
	atomic.AddInt32(&activeDownloads, 1)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "queued",
		"message": "Download queued successfully",
		"id":      newID,
	}); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// Pause endpoint (Protected)
func (h *APIHandler) Pause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	if err := h.service.Pause(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "paused", "id": id}); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// Resume endpoint (Protected)
func (h *APIHandler) Resume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	if err := h.service.Resume(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "resumed", "id": id}); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// Delete endpoint (Protected)
func (h *APIHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	if err := h.service.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id}); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// List endpoint (Protected)
func (h *APIHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statuses, err := h.service.List()
	if err != nil {
		http.Error(w, "Failed to list downloads: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(statuses); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// History endpoint (Protected)
func (h *APIHandler) History(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history, err := h.service.History()
	if err != nil {
		http.Error(w, "Failed to retrieve history: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(history); err != nil {
		utils.Debug("Failed to encode response: %v", err)
	}
}

// startHTTPServer starts the HTTP server using an existing listener
func startHTTPServer(ln net.Listener, port int, defaultOutputDir string, service core.DownloadService) {
	authToken := ensureAuthToken()
	handler := NewAPIHandler(service, port, defaultOutputDir)

	mux := http.NewServeMux()

	// Register Handlers
	mux.HandleFunc("/health", handler.Health)
	mux.HandleFunc("/events", handler.Events)
	mux.HandleFunc("/download", handler.Download)
	mux.HandleFunc("/pause", handler.Pause)
	mux.HandleFunc("/resume", handler.Resume)
	mux.HandleFunc("/delete", handler.Delete)
	mux.HandleFunc("/list", handler.List)
	mux.HandleFunc("/history", handler.History)

	// Wrap mux with Auth and CORS (CORS outermost to ensure 401/403 include headers)
	serverHandler := corsMiddleware(authMiddleware(authToken, mux))

	server := &http.Server{Handler: serverHandler}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		utils.Debug("HTTP server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS, PUT, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow health check without auth
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow OPTIONS for CORS preflight
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// Check for Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			if strings.HasPrefix(authHeader, "Bearer ") {
				providedToken := strings.TrimPrefix(authHeader, "Bearer ")
				if len(providedToken) == len(token) && subtle.ConstantTimeCompare([]byte(providedToken), []byte(token)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func ensureAuthToken() string {
	tokenFile := filepath.Join(config.GetSurgeDir(), "token")
	data, err := os.ReadFile(tokenFile)
	if err == nil {
		return strings.TrimSpace(string(data))
	}

	// Generate new token
	token := uuid.New().String()
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		utils.Debug("Failed to write token file: %v", err)
	}
	return token
}
