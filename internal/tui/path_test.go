package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/utils"
)

// TestStartDownload_EnforcesAbsolutePath verifies that startDownload forces the path to be absolute.
func TestStartDownload_EnforcesAbsolutePath(t *testing.T) {
	// wd, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "surge-test")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ch := make(chan any, 10)
	pool := download.NewWorkerPool(ch, 1)

	m := RootModel{
		Settings:  config.DefaultSettings(),
		Service:   core.NewLocalDownloadServiceWithInput(pool, ch),
		downloads: []*DownloadModel{},
		list:      NewDownloadList(80, 20), // Initialize list
	}

	// Test case 1: Relative path
	relPath := "subdir"
	url := "http://example.com/file.zip"

	m, _ = m.startDownload(url, nil, nil, relPath, "file.zip", "test-id-1")

	// We expect the new download to be appended
	if len(m.downloads) != 1 {
		t.Fatalf("Expected 1 download, got %d", len(m.downloads))
	}

	dm := m.downloads[0]
	// Verify Destination is absolute (if we updated the code to set it)
	// Currently the code DOES NOT set dm.Destination in startDownload.
	// We should update the code to set it for better UX and testability.

	// If the code is updated, this assertion should pass:
	expected := filepath.Join(utils.EnsureAbsPath(relPath), "file.zip")
	if dm.Destination != expected {
		t.Errorf("Destination not absolute. Got %q, want %q", dm.Destination, expected)
	}
}
