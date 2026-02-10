package cmd

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/tui"
	"github.com/surge-downloader/surge/internal/utils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// Version information - set via ldflags during build
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// activeDownloads tracks the number of currently running downloads in headless mode
var activeDownloads int32

// Globals for Unified Backend
var (
	GlobalPool       *download.WorkerPool
	GlobalProgressCh chan any
	GlobalService    core.DownloadService
	serverProgram    *tea.Program
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "surge [url]...",
	Short:   "An open-source download manager written in Go",
	Long:    `Surge is a blazing fast, open-source terminal (TUI) download manager built in Go.`,
	Version: Version,
	Args:    cobra.ArbitraryArgs,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize Global Progress Channel
		GlobalProgressCh = make(chan any, 100)

		// Initialize Global Worker Pool
		// Load max downloads from settings
		settings, err := config.LoadSettings()
		if err != nil {
			settings = config.DefaultSettings()
		}
		GlobalPool = download.NewWorkerPool(GlobalProgressCh, settings.Connections.MaxConcurrentDownloads)
	},
	Run: func(cmd *cobra.Command, args []string) {
		initializeGlobalState()

		// Attempt to acquire lock
		isMaster, err := AcquireLock()
		if err != nil {
			fmt.Printf("Error acquiring lock: %v\n", err)
			os.Exit(1)
		}

		if !isMaster {
			fmt.Fprintln(os.Stderr, "Error: Surge is already running.")
			fmt.Fprintln(os.Stderr, "Use 'surge add <url>' to add a download to the active instance.")
			os.Exit(1)
		}
		defer func() {
			if err := ReleaseLock(); err != nil {
				utils.Debug("Error releasing lock: %v", err)
			}
		}()

		// Initialize Service
		GlobalService = core.NewLocalDownloadServiceWithInput(GlobalPool, GlobalProgressCh)

		portFlag, _ := cmd.Flags().GetInt("port")
		batchFile, _ := cmd.Flags().GetString("batch")
		outputDir, _ := cmd.Flags().GetString("output")
		noResume, _ := cmd.Flags().GetBool("no-resume")
		exitWhenDone, _ := cmd.Flags().GetBool("exit-when-done")

		var port int
		var listener net.Listener

		if portFlag > 0 {
			// Strict port mode
			port = portFlag
			var err error
			listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: could not bind to port %d: %v\n", port, err)
				os.Exit(1)
			}
		} else {
			// Auto-discovery mode
			port, listener = findAvailablePort(1700)
			if listener == nil {
				fmt.Fprintf(os.Stderr, "Error: could not find available port\n")
				os.Exit(1)
			}
		}

		// Save port for browser extension AND CLI discovery
		saveActivePort(port)
		defer removeActivePort()

		// Start HTTP server in background (reuse the listener)
		go startHTTPServer(listener, port, outputDir, GlobalService)

		// Queue initial downloads if any
		go func() {
			var urls []string
			urls = append(urls, args...)

			if batchFile != "" {
				fileUrls, err := readURLsFromFile(batchFile)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading batch file: %v\n", err)
				} else {
					urls = append(urls, fileUrls...)
				}
			}

			if len(urls) > 0 {
				processDownloads(urls, outputDir, 0) // 0 port = internal direct add
			}
		}()

		// Start TUI (default mode)
		startTUI(port, exitWhenDone, noResume)
	},
}

// startTUI initializes and runs the TUI program
func startTUI(port int, exitWhenDone bool, noResume bool) {
	// Initialize TUI
	// GlobalService and GlobalProgressCh are already initialized in PersistentPreRun or Run

	m := tui.InitialRootModel(port, Version, GlobalService, noResume)
	// m := tui.InitialRootModel(port, Version)
	// No need to instantiate separate pool

	p := tea.NewProgram(m, tea.WithAltScreen())
	serverProgram = p // Save reference for HTTP handler

	// Get event stream from service
	events, cleanup, err := GlobalService.StreamEvents(context.Background())
	if err != nil {
		fmt.Printf("Error getting event stream: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	// Background listener for progress events
	go func() {
		for msg := range events {
			p.Send(msg)
		}
	}()

	// Exit-when-done checker for TUI
	if exitWhenDone {
		go func() {
			// Wait a bit for initial downloads to be queued
			time.Sleep(3 * time.Second)
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if GlobalPool != nil && GlobalPool.ActiveCount() == 0 {
					// Send quit message to TUI
					p.Send(tea.Quit())
					return
				}
			}
		}()
	}

	// Run TUI
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}

// StartHeadlessConsumer starts a goroutine to consume progress messages and log to stdout
func StartHeadlessConsumer() {
	go func() {
		if GlobalService == nil {
			return
		}
		stream, cleanup, err := GlobalService.StreamEvents(context.Background())
		if err != nil {
			utils.Debug("Failed to start event stream: %v", err)
			return
		}
		defer cleanup()

		for msg := range stream {
			switch m := msg.(type) {
			case events.DownloadStartedMsg:
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Started: %s [%s]\n", m.Filename, id)
			case events.DownloadCompleteMsg:
				atomic.AddInt32(&activeDownloads, -1)
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Completed: %s [%s] (in %s)\n", m.Filename, id, m.Elapsed)
			case events.DownloadErrorMsg:
				atomic.AddInt32(&activeDownloads, -1)
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Error: %s [%s]: %v\n", m.Filename, id, m.Err)
			case events.DownloadQueuedMsg:
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Queued: %s [%s]\n", m.Filename, id)
			case events.DownloadPausedMsg:
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Paused: %s [%s]\n", m.Filename, id)
			case events.DownloadResumedMsg:
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Resumed: %s [%s]\n", m.Filename, id)
			case events.DownloadRemovedMsg:
				id := m.DownloadID
				if len(id) > 8 {
					id = id[:8]
				}
				fmt.Printf("Removed: %s [%s]\n", m.Filename, id)
			}
		}
	}()
}

// findAvailablePort tries ports starting from 'start' until one is available
func findAvailablePort(start int) (int, net.Listener) {
	for port := start; port < start+100; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return port, ln
		}
	}
	return 0, nil
}

// saveActivePort writes the active port to ~/.surge/port for extension discovery
func saveActivePort(port int) {
	portFile := filepath.Join(config.GetSurgeDir(), "port")
	if err := os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0o644); err != nil {
		utils.Debug("Error writing port file: %v", err)
	}
	utils.Debug("HTTP server listening on port %d", port)
}

// removeActivePort cleans up the port file on exit
func removeActivePort() {
	portFile := filepath.Join(config.GetSurgeDir(), "port")
	if err := os.Remove(portFile); err != nil && !os.IsNotExist(err) {
		utils.Debug("Error removing port file: %v", err)
	}
}

// startHTTPServer starts the HTTP server using an existing listener
func startHTTPServer(ln net.Listener, port int, defaultOutputDir string, service core.DownloadService) {
	authToken := ensureAuthToken()

	mux := http.NewServeMux()

	// Health check endpoint (Public)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"port":   port,
		}); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
	})

	// SSE Events Endpoint (Protected)
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		// Set headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Get event stream
		stream, cleanup, err := service.StreamEvents(r.Context())
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
	})

	// Download endpoint (Protected + Public for simple GET status if needed? No, let's protect all for now)
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		handleDownload(w, r, defaultOutputDir, service)
	})

	// Pause endpoint (Protected)
	mux.HandleFunc("/pause", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		if err := service.Pause(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "paused", "id": id}); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
	})

	// Resume endpoint (Protected)
	mux.HandleFunc("/resume", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		if err := service.Resume(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "resumed", "id": id}); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
	})

	// Delete endpoint (Protected)
	mux.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete && r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		if err := service.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "id": id}); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
	})

	// List endpoint (Protected)
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		statuses, err := service.List()
		if err != nil {
			http.Error(w, "Failed to list downloads: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(statuses); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
	})

	// History endpoint (Protected)
	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		history, err := service.History()
		if err != nil {
			http.Error(w, "Failed to retrieve history: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(history); err != nil {
			utils.Debug("Failed to encode response: %v", err)
		}
	})

	// Wrap mux with Auth and CORS (CORS outermost to ensure 401/403 include headers)
	handler := corsMiddleware(authMiddleware(authToken, mux))

	server := &http.Server{Handler: handler}
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

func handleDownload(w http.ResponseWriter, r *http.Request, defaultOutputDir string, service core.DownloadService) {
	// GET request to query status
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing id parameter", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if service == nil {
			http.Error(w, "Service unavailable", http.StatusInternalServerError)
			return
		}

		status, err := service.GetStatus(id)
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
	if service == nil {
		http.Error(w, "Service unavailable", http.StatusInternalServerError)
		return
	}

	// Prepare output path
	outPath := req.Path
	if req.RelativeToDefaultDir && req.Path != "" {
		// Resolve relative to default download directory
		baseDir := settings.General.DefaultDownloadDir
		if baseDir == "" {
			baseDir = defaultOutputDir
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
		if defaultOutputDir != "" {
			outPath = defaultOutputDir
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
				if err := service.Publish(events.DownloadRequestMsg{
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
	newID, err := service.Add(urlForAdd, outPath, req.Filename, mirrorsForAdd, req.Headers)
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

// processDownloads handles the logic of adding downloads either to local pool or remote server
// Returns the number of successfully added downloads
func processDownloads(urls []string, outputDir string, port int) int {
	successCount := 0

	// If port > 0, we are sending to a remote server
	// If port > 0, we are sending to a remote server
	if port > 0 {
		for _, arg := range urls {
			url, mirrors := ParseURLArg(arg)
			if url == "" {
				continue
			}
			err := sendToServer(url, mirrors, outputDir, port)
			if err != nil {
				fmt.Printf("Error adding %s: %v\n", url, err)
			} else {
				successCount++
			}
		}
		return successCount
	}

	// Internal add (TUI or Headless mode)
	if GlobalService == nil {
		fmt.Fprintln(os.Stderr, "Error: GlobalService not initialized")
		return 0
	}

	settings, err := config.LoadSettings()
	if err != nil {
		settings = config.DefaultSettings()
	}

	for _, arg := range urls {
		// Validation
		if arg == "" {
			continue
		}

		url, mirrors := ParseURLArg(arg)
		if url == "" {
			continue
		}

		// Prepare output path
		outPath := outputDir
		if outPath == "" {
			if settings.General.DefaultDownloadDir != "" {
				outPath = settings.General.DefaultDownloadDir
				_ = os.MkdirAll(outPath, 0o755)
			} else {
				outPath = "."
			}
		}
		outPath = utils.EnsureAbsPath(outPath)

		// Check for duplicates/extensions if we are in TUI mode (serverProgram != nil)
		// For headless/root direct add, we might skip prompt or auto-approve?
		// For now, let's just add directly if headless, or prompt if TUI is up.

		// If TUI is up (serverProgram != nil), we might want to send a request msg?
		// But processDownloads is called from QUEUE init routine, primarily for CLI args.
		// If CLI args provided, user probably wants them added immediately.

		_, err := GlobalService.Add(url, outPath, "", mirrors, nil)
		if err != nil {
			fmt.Printf("Error adding %s: %v\n", url, err)
			continue
		}
		atomic.AddInt32(&activeDownloads, 1)
		successCount++
	}
	return successCount
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringP("batch", "b", "", "File containing URLs to download (one per line)")
	rootCmd.Flags().IntP("port", "p", 0, "Port to listen on (default: 8080 or first available)")
	rootCmd.Flags().StringP("output", "o", "", "Default output directory")
	rootCmd.Flags().Bool("no-resume", false, "Do not auto-resume paused downloads on startup")
	rootCmd.Flags().Bool("exit-when-done", false, "Exit when all downloads complete")
	rootCmd.SetVersionTemplate("Surge version {{.Version}}\n")
}

// initializeGlobalState sets up the environment and configures the engine state and logging
func initializeGlobalState() {
	stateDir := config.GetStateDir()
	logsDir := config.GetLogsDir()

	// Ensure directories exist
	_ = os.MkdirAll(stateDir, 0o755)
	_ = os.MkdirAll(logsDir, 0o755)

	// Config engine state
	state.Configure(filepath.Join(stateDir, "surge.db"))

	// Config logging
	utils.ConfigureDebug(logsDir)

	// Clean up old logs
	settings, err := config.LoadSettings()
	var retention int
	if err == nil {
		retention = settings.General.LogRetentionCount
	} else {
		retention = config.DefaultSettings().General.LogRetentionCount
	}
	utils.CleanupLogs(retention)
}

func resumePausedDownloads() {
	settings, err := config.LoadSettings()
	if err != nil {
		return // Can't check preference
	}

	pausedEntries, err := state.LoadPausedDownloads()
	if err != nil {
		return
	}

	for _, entry := range pausedEntries {
		// If entry is explicitly queued, we should start it regardless of AutoResume setting
		// If entry is paused, we only start it if AutoResume is enabled
		if entry.Status == "paused" && !settings.General.AutoResume {
			continue
		}
		if GlobalService == nil || entry.ID == "" {
			continue
		}
		if err := GlobalService.Resume(entry.ID); err == nil {
			atomic.AddInt32(&activeDownloads, 1)
		}
	}
}
