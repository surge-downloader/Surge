package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestCmd_AutoResume_Execution(t *testing.T) {
	// 1. Setup Environment
	tmpDir, err := os.MkdirTemp("", "surge-cmd-resume-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() {
		if originalXDG == "" {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			_ = os.Setenv("XDG_CONFIG_HOME", originalXDG)
		}
	}()

	surgeDir := config.GetSurgeDir()
	if err := os.MkdirAll(surgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Settings with AutoResume = true
	settingsPath := filepath.Join(surgeDir, "settings.json")
	settings := config.DefaultSettings()
	settings.General.AutoResume = true
	settings.General.DefaultDownloadDir = tmpDir

	data, _ := json.Marshal(settings)
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Configure State DB
	state.CloseDB() // Ensure clean state
	dbPath := filepath.Join(surgeDir, "state", "surge.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	state.Configure(dbPath)

	// 4. Seed DB with a paused download
	testID := "cmd-resume-id-1"
	testURL := "http://example.com/cmd-resume.zip"
	testDest := filepath.Join(tmpDir, "cmd-resume.zip")

	manualState := &types.DownloadState{
		ID:         testID,
		URL:        testURL,
		Filename:   "cmd-resume.zip",
		DestPath:   testDest,
		TotalSize:  1000,
		Downloaded: 500,
		PausedAt:   time.Now().Unix(),
		CreatedAt:  time.Now().Unix(),
	}
	if err := state.SaveState(testURL, testDest, manualState); err != nil {
		t.Fatal(err)
	}

	// 5. Initialize GlobalPool
	GlobalProgressCh = make(chan any, 10)
	GlobalPool = download.NewWorkerPool(GlobalProgressCh, 4)

	// 6. Call the function
	resumePausedDownloads()

	// 7. Verify
	// Check if GlobalPool has the download active
	if !GlobalPool.HasDownload(testURL) {
		t.Error("Download was not added to GlobalPool by resumePausedDownloads")
	}
}
