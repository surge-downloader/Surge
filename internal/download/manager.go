package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/surge-downloader/surge/internal/engine"
	"github.com/surge-downloader/surge/internal/engine/concurrent"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/single"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

// ProbeResult contains all metadata from server probe
type ProbeResult struct {
	FileSize      int64
	SupportsRange bool
	Filename      string
	ContentType   string
}

// probeServer has been moved to internal/engine/probe.go

// uniqueFilePath returns a unique file path by appending (1), (2), etc. if the file exists
func uniqueFilePath(path string) string {
	// Check if file exists (both final and incomplete)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err := os.Stat(path + types.IncompleteSuffix); os.IsNotExist(err) {
			return path // Neither exists, use original
		}
	}

	// File exists, generate unique name
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	name := strings.TrimSuffix(filepath.Base(path), ext)

	// Check if name already has a counter like "file(1)"
	base := name
	counter := 1

	// Clean name to ensure parsing works even with trailing spaces
	cleanName := strings.TrimSpace(name)
	if len(cleanName) > 3 && cleanName[len(cleanName)-1] == ')' {
		if openParen := strings.LastIndexByte(cleanName, '('); openParen != -1 {
			// Try to parse number between parens
			numStr := cleanName[openParen+1 : len(cleanName)-1]
			if num, err := strconv.Atoi(numStr); err == nil && num > 0 {
				base = cleanName[:openParen]
				// Preserve original whitespace in base if it was "file (1)" -> "file "
				// But we trimmed name. Let's rely on string slicing of cleanName?
				// No, if cleanName was trimmed, base might differ from "name".
				// But we construct new name using "base".
				counter = num + 1
			}
		}
	}

	for i := 0; i < 100; i++ { // Try next 100 numbers
		candidate := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, counter+i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			if _, err := os.Stat(candidate + types.IncompleteSuffix); os.IsNotExist(err) {
				return candidate
			}
		}
	}

	// Fallback: just append a large random number or give up (original behavior essentially gave up or made ugly names)
	// Here we fallback to original behavior of appending if the clean one failed 100 times
	return path
}

// TUIDownload is the main entry point for TUI downloads
func TUIDownload(ctx context.Context, cfg *types.DownloadConfig) error {
	// Probe server once to get all metadata
	utils.Debug("TUIDownload: Probing server... %s", cfg.URL)
	probe, err := engine.ProbeServer(ctx, cfg.URL, cfg.Filename, cfg.Headers)
	if err != nil {
		utils.Debug("TUIDownload: Probe failed: %v\n", err)
		return err
	}
	utils.Debug("TUIDownload: Probe success %d", probe.FileSize)

	// Start download timer (exclude probing time)
	start := time.Now()
	defer func() {
		utils.Debug("Download %s completed in %v", cfg.URL, time.Since(start))
	}()

	// Construct proper output path
	destPath := cfg.OutputPath

	// Auto-create output directory if it doesn't exist
	if _, err := os.Stat(cfg.OutputPath); os.IsNotExist(err) {
		if mkErr := os.MkdirAll(cfg.OutputPath, 0o755); mkErr != nil {
			utils.Debug("Failed to create output directory: %v", mkErr)
		}
	}

	if info, err := os.Stat(cfg.OutputPath); err == nil && info.IsDir() {
		// Use cfg.Filename if TUI provided one, otherwise use probe.Filename
		filename := probe.Filename
		if cfg.Filename != "" {
			filename = cfg.Filename
		}
		destPath = filepath.Join(cfg.OutputPath, filename)
	}

	// Check if this is a resume (explicitly marked by TUI)
	var savedState *types.DownloadState
	if cfg.IsResume && cfg.DestPath != "" {
		// Resume: use the provided destination path for state lookup
		savedState, _ = state.LoadState(cfg.URL, cfg.DestPath)

		// Restore mirrors from state if found
		if savedState != nil && len(savedState.Mirrors) > 0 {
			// Create map of existing mirrors to avoid duplicates
			existing := make(map[string]bool)
			for _, m := range cfg.Mirrors {
				existing[m] = true
			}

			// Add restored mirrors
			for _, m := range savedState.Mirrors {
				if !existing[m] {
					cfg.Mirrors = append(cfg.Mirrors, m)
					existing[m] = true
				}
			}
			utils.Debug("Restored %d mirrors from state", len(savedState.Mirrors))
		}
	}
	isResume := cfg.IsResume && savedState != nil && savedState.DestPath != ""

	if isResume {
		// Resume: use saved destination path directly (don't generate new unique name)
		destPath = savedState.DestPath
		utils.Debug("Resuming download, using saved destPath: %s", destPath)
	} else {
		// Fresh download without TUI-provided filename: generate unique filename if file already exists
		destPath = uniqueFilePath(destPath)
	}
	finalFilename := filepath.Base(destPath)
	utils.Debug("Destination path: %s", destPath)

	// Update filename in config so caller (WorkerPool) sees it
	cfg.Filename = finalFilename
	cfg.DestPath = destPath // Save resolved path for resume logic (WorkerPool)

	// Send download started message
	if cfg.ProgressCh != nil {
		cfg.ProgressCh <- events.DownloadStartedMsg{
			DownloadID: cfg.ID,
			URL:        cfg.URL,
			Filename:   finalFilename,
			Total:      probe.FileSize,
			DestPath:   destPath,
			State:      cfg.State,
		}
	}

	// Update shared state
	if cfg.State != nil {
		cfg.State.SetTotalSize(probe.FileSize)
	}

	// Choose downloader based on probe results
	var downloadErr error
	if probe.SupportsRange && probe.FileSize > 0 {
		utils.Debug("Using concurrent downloader")

		// We probe all candidate mirrors (cfg.Mirrors) to filter out invalid ones
		var activeMirrors []string
		if len(cfg.Mirrors) > 0 {
			utils.Debug("Probing %d mirrors", len(cfg.Mirrors))
			// Always check primary + mirrors to ensure we are using the best set
			allToCheck := append([]string{cfg.URL}, cfg.Mirrors...)
			valid, errs := engine.ProbeMirrors(ctx, allToCheck)

			// Log errors
			for u, e := range errs {
				utils.Debug("Mirror probe failed for %s: %v", u, e)
			}

			// Filter valid mirrors (excluding primary as it is handled separately)
			for _, v := range valid {
				if v != cfg.URL {
					activeMirrors = append(activeMirrors, v)
				}
			}
			utils.Debug("Found %d active mirrors from %d candidates", len(activeMirrors), len(cfg.Mirrors))
		}

		d := concurrent.NewConcurrentDownloader(cfg.ID, cfg.ProgressCh, cfg.State, cfg.Runtime)
		d.Headers = cfg.Headers // Forward custom headers from browser extension
		utils.Debug("Calling Download with mirrors: %v", cfg.Mirrors)
		downloadErr = d.Download(ctx, cfg.URL, cfg.Mirrors, activeMirrors, destPath, probe.FileSize, cfg.Verbose)
	} else {
		// Fallback to single-threaded downloader
		utils.Debug("Using single-threaded downloader")
		d := single.NewSingleDownloader(cfg.ID, cfg.ProgressCh, cfg.State, cfg.Runtime)
		d.Headers = cfg.Headers // Forward custom headers from browser extension
		downloadErr = d.Download(ctx, cfg.URL, destPath, probe.FileSize, probe.Filename, cfg.Verbose)
	}

	// Only send completion if NO error AND not paused
	// Check specifically for ErrPaused to avoid treating it as error
	if errors.Is(downloadErr, types.ErrPaused) {
		utils.Debug("Download paused cleanly")
		return nil // Return nil so worker can remove it from active map
	}

	isPaused := cfg.State != nil && cfg.State.IsPaused()
	if downloadErr == nil && !isPaused {
		elapsed := time.Since(start)
		// For resumed downloads, add previously saved elapsed time
		if cfg.State != nil && cfg.State.SavedElapsed > 0 {
			elapsed += cfg.State.SavedElapsed
		}

		// Persist to history before sending event
		if err := state.AddToMasterList(types.DownloadEntry{
			ID:          cfg.ID,
			URL:         cfg.URL,
			URLHash:     state.URLHash(cfg.URL),
			DestPath:    destPath,
			Filename:    finalFilename,
			Status:      "completed",
			TotalSize:   probe.FileSize,
			Downloaded:  probe.FileSize,
			CompletedAt: time.Now().Unix(),
			TimeTaken:   elapsed.Milliseconds(),
		}); err != nil {
			utils.Debug("Failed to persist completed download: %v", err)
		}

		if cfg.ProgressCh != nil {
			cfg.ProgressCh <- events.DownloadCompleteMsg{
				DownloadID: cfg.ID,
				Filename:   finalFilename,
				Elapsed:    elapsed,
				Total:      probe.FileSize,
			}
		}
	} else if downloadErr != nil && !isPaused {
		// Persist error state
		if err := state.AddToMasterList(types.DownloadEntry{
			ID:         cfg.ID,
			URL:        cfg.URL,
			URLHash:    state.URLHash(cfg.URL),
			DestPath:   destPath,
			Filename:   finalFilename,
			Status:     "error",
			TotalSize:  probe.FileSize,
			Downloaded: cfg.State.Downloaded.Load(),
		}); err != nil {
			utils.Debug("Failed to persist error state: %v", err)
		}
	}

	return downloadErr
}

// Download is the CLI entry point (non-TUI) - convenience wrapper
func Download(ctx context.Context, url, outPath string, verbose bool, progressCh chan<- any, id string) error {
	cfg := types.DownloadConfig{
		URL:        url,
		OutputPath: outPath,
		ID:         id,
		Verbose:    verbose,
		ProgressCh: progressCh,
		State:      nil,
	}
	return TUIDownload(ctx, &cfg)
}
