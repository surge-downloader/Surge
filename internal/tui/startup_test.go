package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// TestTUI_Startup_HandlesResume verifies that TUI initialization handles resume logic correctly
// including "queued" items and AutoResume settings.
func TestTUI_Startup_HandlesResume(t *testing.T) {
	// 1. Setup Environment
	tmpDir, err := os.MkdirTemp("", "surge-tui-startup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	setupTestEnv(t, tmpDir)

	// 2. Seed DB with a 'queued' download (as set by 'surge resume' offline)
	testID := "tui-resume-id"
	testURL := "http://example.com/tui-resume.zip"
	testDest := filepath.Join(tmpDir, "tui-resume.zip")
	seedDownload(t, testID, testURL, testDest, "queued")

	// 3. Initialize TUI Model (Simulate StartTUI)
	progressChan := make(chan any, 10)
	pool := download.NewWorkerPool(progressChan, 3)

	// PASSING noResume=false (default)
	m := InitialRootModel(1700, "test-version", core.NewLocalDownloadServiceWithInput(pool, progressChan), false)

	// 4. Verify Download is Active in Model
	// InitialRootModel loads downloads and should set paused=false for "queued" items
	found := false
	for _, d := range m.downloads { // Access unexported field
		if d.ID == testID {
			found = true
			if !d.pendingResume {
				t.Error("TUI Model initialized queued download without pendingResume=true")
			}
			// Note: d.paused will be true initially until async resume completes
			// Verify Filename and Destination are preserved (critical to avoid uniqueFilePath generation)
			if d.Filename != "tui-resume.zip" {
				t.Errorf("Expected filename tui-resume.zip, got %s", d.Filename)
			}
			if d.Destination != testDest {
				t.Errorf("Expected destination %s, got %s", d.Destination, d.Destination)
			}
		}
	}
	if !found {
		t.Error("TUI Model failed to load queued download")
	}

	// 5. Verify it was added to Pool
	// We can't rely on pool immediate state as worker is async, but Model state reflects intent
}

// Helper functions (duplicated from cmd/startup_test.go because packages differ)
func setupTestEnv(t *testing.T, tmpDir string) {
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Cleanup(func() {
		if originalXDG == "" {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			_ = os.Setenv("XDG_CONFIG_HOME", originalXDG)
		}
	})

	surgeDir := config.GetSurgeDir()
	if err := os.MkdirAll(surgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Setup Settings (AutoResume=false default)
	settings := config.DefaultSettings()
	settings.General.AutoResume = false // Ensure we test that "queued" overrides this
	if err := config.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Configure DB
	dbPath := filepath.Join(surgeDir, "state", "surge.db")
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	state.CloseDB()
	state.Configure(dbPath)
}

func seedDownload(t *testing.T, id, url, dest, status string) {
	manualState := &types.DownloadState{
		ID:         id,
		URL:        url,
		Filename:   filepath.Base(dest),
		DestPath:   dest,
		TotalSize:  1000,
		Downloaded: 0,
		PausedAt:   0,
		CreatedAt:  time.Now().Unix(),
	}
	if err := state.SaveState(url, dest, manualState); err != nil {
		t.Fatal(err)
	}
	if err := state.UpdateStatus(id, status); err != nil {
		t.Fatal(err)
	}
}
