package cmd

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

// TestServer_Startup_HandlesResume verifies that resumePausedDownloads() works for server mode
func TestServer_Startup_HandlesResume(t *testing.T) {
	// 1. Setup Environment
	tmpDir, err := os.MkdirTemp("", "surge-server-startup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	setupTestEnv(t, tmpDir)

	// 2. Seed DB with 'queued' download
	testID := "server-resume-id"
	testURL := "http://example.com/server-resume.zip"
	testDest := filepath.Join(tmpDir, "server-resume.zip")
	seedDownload(t, testID, testURL, testDest, "queued")

	// 3. Initialize Global Pool (required for resumePausedDownloads)
	GlobalProgressCh = make(chan any, 10)
	GlobalPool = download.NewWorkerPool(GlobalProgressCh, 3)
	GlobalService = core.NewLocalDownloadServiceWithInput(GlobalPool, GlobalProgressCh)

	// 4. Run Resume Logic (Simulate Server Start)
	resumePausedDownloads()

	// 5. Verify Download is in GlobalPool
	status := GlobalPool.GetStatus(testID)
	// GetStatus checks active downloads. If it returned non-nil, it's active!
	if status == nil {
		// Check if it's in queued map (GetStatus checks both active and queued internal maps)
		// Wait, GetStatus implementation in pool.go checks p.downloads and p.queued
		t.Fatal("Download not found in GlobalPool after resumePausedDownloads()")
	}

	if status.Status != "queued" && status.Status != "downloading" {
		t.Errorf("Expected status queued/downloading, got %s", status.Status)
	}
}

// Helper: Setup XDG_CONFIG_HOME and Settings
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
