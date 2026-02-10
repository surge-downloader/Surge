package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// TestStateSync verifies that the TUI uses the shared state object
// from the worker, allowing external progress updates to be seen.
func TestStateSync(t *testing.T) {
	// Setup temp DB to avoid auto-resuming real downloads (which causes panic if pool is nil)
	tmpDir, err := os.MkdirTemp("", "surge-test-sync")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := filepath.Join(tmpDir, "surge.db")
	state.Configure(dbPath)
	defer state.CloseDB()

	// Provide a dummy pool to avoid panics if logic tries to use it
	progressChan := make(chan any, 10)
	pool := download.NewWorkerPool(progressChan, 1)

	// Initialize model with progress channel and service
	m := InitialRootModel(1700, "test-version", core.NewLocalDownloadServiceWithInput(pool, progressChan), false)

	downloadID := "external-id"
	// Create the "worker" state - this is the source of truth
	workerState := types.NewProgressState(downloadID, 1000)

	p := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithInput(nil))

	go func() {
		// Simulate download start (from external source)
		// Current implementation of DownloadStartedMsg doesn't carry state
		// So TUI will create its own state (BUG).
		time.Sleep(200 * time.Millisecond)
		p.Send(events.DownloadStartedMsg{
			DownloadID: downloadID,
			Filename:   "external.file",
			Total:      1000,
			URL:        "http://example.com/external",
			DestPath:   "/tmp/external.file",
			State:      workerState,
		})

		// Simulate worker updating the state -> Send Progress Event
		// Note: The ProgressReporter reads from VerifiedProgress (via GetProgress)
		time.Sleep(300 * time.Millisecond)
		workerState.VerifiedProgress.Store(500)
		p.Send(events.ProgressMsg{
			DownloadID: downloadID,
			Downloaded: 500,
			Total:      1000,
			Speed:      100, // Dummy speed
			Elapsed:    10 * time.Second,
		})

		// Wait effectively for 2 poll cycles (150ms * 2 = 300ms) + buffer
		time.Sleep(500 * time.Millisecond)
		p.Quit()
	}()

	finalModel, err := p.Run()
	if err != nil {
		t.Fatalf("Program failed: %v", err)
	}

	finalRoot := finalModel.(RootModel)
	var target *DownloadModel
	for _, d := range finalRoot.downloads {
		if d.ID == downloadID {
			target = d
			break
		}
	}

	if target == nil {
		t.Fatal("Download model not found")
	}

	// Without fix: TUI creates its own state, so Downloaded stays 0
	// With fix: TUI uses workerState, so Downloaded becomes 500
	if target.Downloaded != 500 {
		t.Errorf("State not synced. TUI Downloaded=%d, Worker Downloaded=500", target.Downloaded)
	}
}
