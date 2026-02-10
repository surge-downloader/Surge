package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
