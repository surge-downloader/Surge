package tui

import (
	"encoding/json"
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

func TestAutoResume_Enabled(t *testing.T) {
	// 1. Setup Environment with XDG_CONFIG_HOME override
	tmpDir, err := os.MkdirTemp("", "surge-autoresume-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Override config home
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() {
		if originalXDG == "" {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			_ = os.Setenv("XDG_CONFIG_HOME", originalXDG)
		}
	}()

	// config.GetSurgeDir() will now be under tmpDir/surge
	surgeDir := config.GetSurgeDir()
	if err := os.MkdirAll(surgeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Create settings file with AutoResume = true
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
	testID := "resume-id-1"
	testURL := "http://example.com/resume.zip"
	testDest := filepath.Join(tmpDir, "resume.zip")

	manualState := &types.DownloadState{
		ID:         testID,
		URL:        testURL,
		Filename:   "resume.zip",
		DestPath:   testDest,
		TotalSize:  1000,
		Downloaded: 500,
		PausedAt:   time.Now().Unix(),
		CreatedAt:  time.Now().Unix(),
	}
	if err := state.SaveState(testURL, testDest, manualState); err != nil {
		t.Fatal(err)
	}

	// 5. Initialize Model
	ch := make(chan any, 10)
	pool := download.NewWorkerPool(ch, 1)

	m := InitialRootModel(1700, "test-version", core.NewLocalDownloadServiceWithInput(pool, ch), false)

	// 6. Verify Download is Resumed
	found := false
	for _, d := range m.downloads {
		if d.ID == testID {
			found = true
			if !d.pendingResume {
				t.Error("Download should have pendingResume=true when AutoResume is enabled")
			}
			// It starts as paused, waiting for Init() to resume
		}
	}

	if !found {
		t.Error("Paused download was not loaded into the model")
	}
}

func TestAutoResume_Disabled(t *testing.T) {
	// 1. Setup Environment
	tmpDir, err := os.MkdirTemp("", "surge-autoresume-off-test")
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

	// 2. Settings with AutoResume = false
	settingsPath := filepath.Join(surgeDir, "settings.json")
	settings := config.DefaultSettings()
	settings.General.AutoResume = false

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
	testID := "resume-id-2"
	testURL := "http://example.com/resume2.zip"
	testDest := filepath.Join(tmpDir, "resume2.zip")

	manualState := &types.DownloadState{
		ID:         testID,
		URL:        testURL,
		Filename:   "resume2.zip",
		DestPath:   testDest,
		TotalSize:  1000,
		Downloaded: 500,
		PausedAt:   time.Now().Unix(),
		CreatedAt:  time.Now().Unix(),
	}
	if err := state.SaveState(testURL, testDest, manualState); err != nil {
		t.Fatal(err)
	}

	// 5. Initialize Model
	ch := make(chan any, 10)
	pool := download.NewWorkerPool(ch, 1)

	m := InitialRootModel(1700, "test-version", core.NewLocalDownloadServiceWithInput(pool, ch), false)

	// 6. Verify Download is Resumed
	found := false
	for _, d := range m.downloads {
		if d.ID == testID {
			found = true
			if !d.paused {
				t.Error("Download SHOULD be paused when AutoResume is disabled")
			}
		}
	}

	if !found {
		t.Error("Paused download was not loaded into the model")
	}
}
