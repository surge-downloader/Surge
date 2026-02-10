package download_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
	"github.com/surge-downloader/surge/internal/utils"
)

func TestIntegration_MirrorResume(t *testing.T) {
	// 1. Setup temporary directory for DB and downloads
	tmpDir, err := os.MkdirTemp("", "surge-mirror-resume-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Set XDG_CONFIG_HOME to tmpDir so state.GetDB() creates DB there
	// The config package uses "surge" subdirectory
	configDir := tmpDir // XDG_CONFIG_HOME usually contains the app dir
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Configure debug
	utils.ConfigureDebug(tmpDir)

	// Ensure clean state
	state.CloseDB()
	dbPath := filepath.Join(tmpDir, "surge.db")
	state.Configure(dbPath)
	if _, err := state.GetDB(); err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	defer state.CloseDB()

	// 2. Setup Mock Servers (Primary + Mirror)
	fileSize := int64(200 * 1024 * 1024) // 200MB
	primary := testutil.NewStreamingMockServerT(t,
		fileSize,
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(20*time.Microsecond), // Slow down to ensure we can pause
	)
	defer primary.Close()

	mirror := testutil.NewStreamingMockServerT(t,
		fileSize,
		testutil.WithRangeSupport(true),
		testutil.WithByteLatency(20*time.Microsecond),
	)
	defer mirror.Close()

	// 3. Start Download with Mirror
	ctx := context.Background()
	progressCh := make(chan any, 100)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 4,
	}
	progState := types.NewProgressState(uuid.New().String(), fileSize)

	filename := "mirrorfile.bin"
	outputPath := tmpDir
	destPath := filepath.Join(outputPath, filename)

	cfg := types.DownloadConfig{
		URL:        primary.URL(),
		OutputPath: outputPath,
		Filename:   filename,
		ID:         progState.ID,
		ProgressCh: progressCh,
		State:      progState,
		Runtime:    runtime,
		IsResume:   false,
		Mirrors:    []string{mirror.URL()}, // Pass mirror
	}

	// Start download and interrupt
	errCh := make(chan error)
	go func() {
		errCh <- download.TUIDownload(ctx, &cfg)
	}()

	// Wait for some progress
	time.Sleep(200 * time.Millisecond) // Give it time to start and probe

	// Interrupt!
	progState.Pause()

	// Wait for return
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Download did not return")
	}

	// 4. Verify Mirrors Saved
	savedState, err := state.LoadState(primary.URL(), destPath)
	if err != nil || len(savedState.Mirrors) == 0 {
		// Print debug logs
		entries, _ := os.ReadDir(tmpDir)
		for _, e := range entries {
			if !e.IsDir() {
				if e.Name() == filename {
					t.Logf("File %s exists (size: %s)", e.Name(), "200MB")
					continue
				}
				content, _ := os.ReadFile(filepath.Join(tmpDir, e.Name()))
				t.Logf("File %s:\n%s", e.Name(), string(content))
			}
		}
	}
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}
	if len(savedState.Mirrors) == 0 {
		t.Fatal("Mirrors not saved in state!")
	}
	if savedState.Mirrors[0] != mirror.URL() {
		t.Errorf("Saved mirror mismatch. Want %s, got %v", mirror.URL(), savedState.Mirrors)
	}

	// 5. Resume without explicit mirrors
	// Create new config simulating a resumption where we don't know the mirrors initially
	resumeCfg := types.DownloadConfig{
		URL:        primary.URL(),
		OutputPath: outputPath,
		Filename:   filename,
		ID:         progState.ID,
		ProgressCh: progressCh,
		State:      progState,
		Runtime:    runtime,
		IsResume:   true,
		DestPath:   destPath,
		Mirrors:    []string{}, // Empty mirrors!
	}

	progState.Resume() // Reset pause flag

	// We can't easily hook into TUIDownload to verify it loaded mirrors without running it.
	go func() {
		errCh <- download.TUIDownload(ctx, &resumeCfg)
	}()

	// Give it a moment to load state
	time.Sleep(200 * time.Millisecond)
	progState.Pause()
	<-errCh

	// Check if resumeCfg.Mirrors was updated?
	// Since resumeCfg is passed by pointer, it should be updated if TUIDownload modifies it.
	if len(resumeCfg.Mirrors) == 0 {
		t.Fatal("resumeCfg.Mirrors was not updated from saved state")
	}
	if resumeCfg.Mirrors[0] != mirror.URL() {
		t.Errorf("Resume config mirror mismatch. Want %s, got %v", mirror.URL(), resumeCfg.Mirrors)
	}
}
