package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/junaid2005p/surge/internal/config"
	"github.com/junaid2005p/surge/internal/tui"
	"github.com/junaid2005p/surge/internal/utils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// Version information - set via ldflags during build
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// serverProgram holds the TUI program for sending messages from HTTP handler
var serverProgram *tea.Program

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "surge",
	Short:   "An open-source download manager written in Go",
	Long:    `Surge is a fast, concurrent download manager with pause/resume support.`,
	Version: Version,
	Run: func(cmd *cobra.Command, args []string) {
		// Find an available port starting from default
		port, listener := findAvailablePort(8080)
		if listener == nil {
			fmt.Fprintf(os.Stderr, "Error: could not find available port\n")
			os.Exit(1)
		}

		// Save port for browser extension to discover
		saveActivePort(port)

		// Create TUI program
		model := tui.InitialRootModel()
		serverProgram = tea.NewProgram(model, tea.WithAltScreen())

		// Start HTTP server in background (reuse the listener)
		go startHTTPServer(listener, port)

		// Run the TUI (blocking)
		if _, err := serverProgram.Run(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		// Cleanup port file on exit
		removeActivePort()
	},
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
	os.WriteFile(portFile, []byte(fmt.Sprintf("%d", port)), 0644)
	utils.Debug("HTTP server listening on port %d", port)
}

// removeActivePort cleans up the port file on exit
func removeActivePort() {
	portFile := filepath.Join(config.GetSurgeDir(), "port")
	os.Remove(portFile)
}

// startHTTPServer starts the HTTP server using an existing listener
func startHTTPServer(ln net.Listener, port int) {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"port":   port,
		})
	})

	// Download endpoint
	mux.HandleFunc("/download", handleDownload)

	server := &http.Server{Handler: corsMiddleware(mux)}
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		utils.Debug("HTTP server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}

// DownloadRequest represents a download request from the browser extension
type DownloadRequest struct {
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
	Path     string `json:"path,omitempty"`
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

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
	if filepath.IsAbs(req.Path) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Don't default to "." here, let TUI handle it
	// if req.Path == "" {
	// 	req.Path = "."
	// }

	utils.Debug("Received download request: URL=%s, Path=%s", req.URL, req.Path)

	// Send message to TUI to start download
	serverProgram.Send(tui.StartDownloadMsg{
		URL:      req.URL,
		Path:     req.Path,
		Filename: req.Filename,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "queued",
		"message": "Download request received",
	})
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(getCmd)
	rootCmd.SetVersionTemplate("Surge version {{.Version}}\n")
}
