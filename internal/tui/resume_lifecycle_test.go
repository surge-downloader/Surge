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
	"github.com/surge-downloader/surge/internal/utils"
)

// TestResume_RespectsOriginalPath_WhenDefaultChanges verifies that a download
// started with one default directory keeps its absolute path when resumed,
// even if the default directory setting changes in the meantime.
func TestResume_RespectsOriginalPath_WhenDefaultChanges(t *testing.T) {
	// 1. Setup Environment
	tmpDir, err := os.MkdirTemp("", "surge-resume-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create two distinct "default" directories
	dirA := filepath.Join(tmpDir, "DirA")
	dirB := filepath.Join(tmpDir, "DirB")
	_ = os.MkdirAll(dirA, 0o755)
	_ = os.MkdirAll(dirB, 0o755)

	// Setup a temporary DB for state
	state.CloseDB()
	dbPath := filepath.Join(tmpDir, "surge.db")
	state.Configure(dbPath)

	ch := make(chan any, 10)
	pool := download.NewWorkerPool(ch, 1)

	// 2. Initialize Model with DefaultDir = DirA
	settings := config.DefaultSettings()
	settings.General.DefaultDownloadDir = dirA

	m := RootModel{
		Settings:  settings,
		Service:   core.NewLocalDownloadServiceWithInput(pool, ch),
		downloads: []*DownloadModel{},
		list:      NewDownloadList(80, 20), // Initialize list to prevent panic
	}

	// 3. Start a download (simulating "surge get <url>" or TUI add)
	// Change CWD to DirA to simulate "running from DirA"
	originalWd, _ := os.Getwd()
	if err := os.Chdir(dirA); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	testURL := "http://example.com/file.zip"
	testFilename := "file.zip"

	// Start download with relative path "."
	m, _ = m.startDownload(testURL, nil, nil, ".", testFilename, "id-1")

	// 4. Verify Immediate State
	if len(m.downloads) != 1 {
		t.Fatalf("Download not started")
	}
	dm := m.downloads[0]

	expectedPathA := filepath.Join(dirA, testFilename)

	// We expect the Destination to be absolute path
	if dm.Destination != expectedPathA {
		// Resolve symlinks just in case
		evalA, _ := filepath.EvalSymlinks(expectedPathA)
		evalDest, _ := filepath.EvalSymlinks(dm.Destination)
		if evalDest != evalA {
			t.Errorf("Initial download path mismatch.\nGot:  %s\nWant: %s", dm.Destination, expectedPathA)
		}
	}

	// 5. Simulate "Pause" / Persistence
	// Use SaveState to save the paused state (which updates the downloads table with status=paused)
	manualState := &types.DownloadState{
		ID:         dm.ID,
		URL:        dm.URL,
		Filename:   dm.Filename,
		DestPath:   dm.Destination,
		TotalSize:  0,
		Downloaded: 0,
		PausedAt:   time.Now().Unix(),
		CreatedAt:  time.Now().Unix(),
	}
	err = state.SaveState(dm.URL, dm.Destination, manualState)
	if err != nil {
		t.Fatal(err)
	}

	// 6. Change Settings (Default Dir = DirB) and CWD
	settings.General.DefaultDownloadDir = dirB
	if err := os.Chdir(dirB); err != nil {
		t.Fatal(err)
	}

	// 7. Simulate Resume logic
	paused, err := state.LoadPausedDownloads()
	if err != nil {
		t.Fatal(err)
	}

	if len(paused) != 1 {
		t.Fatalf("Expected 1 paused download, got %d", len(paused))
	}

	entry := paused[0]

	// 8. The CRITICAL CHECK
	// The loaded entry.DestPath MUST be DirA, not DirB
	if entry.DestPath != expectedPathA {
		t.Errorf("Resumed path incorrect.\nGot:  %s\nWant: %s", entry.DestPath, expectedPathA)
	}

	// Verify that if we constructed a RuntimeConfig/DownloadConfig, it would use this absolute path
	outputPath := filepath.Dir(entry.DestPath)
	// Even if logic checks for empty/dot, filepath.Dir of absolute path is absolute path.
	if outputPath == "" || outputPath == "." {
		// This should NOT happen for absolute paths
		outputPath = settings.General.DefaultDownloadDir
	}

	// Ensure outputPath resolves to DirA
	outAbs := utils.EnsureAbsPath(outputPath)
	evalLoaded, _ := filepath.EvalSymlinks(outAbs)
	evalDirA, _ := filepath.EvalSymlinks(dirA)

	if evalLoaded != evalDirA {
		t.Errorf("Constructed OutputPath is wrong.\nGot:  %s\nWant: %s", outAbs, dirA)
	}
}
