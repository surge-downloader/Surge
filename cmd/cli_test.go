package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
)

// TestResolveDownloadID_Remote verifies that resolveDownloadID queries the server
func TestResolveDownloadID_Remote(t *testing.T) {
	// 1. Mock Server
	downloads := []types.DownloadStatus{
		{ID: "aabbccdd-1234-5678-90ab-cdef12345678", Filename: "test_remote.zip"},
	}

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			_ = json.NewEncoder(w).Encode(downloads)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Extract port
	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	// 2. Mock active port file

	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HOME", tempDir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}
	state.Configure(filepath.Join(tempDir, "surge.db"))
	saveActivePort(port)
	defer removeActivePort()

	// 3. Test resolveDownloadID
	partial := "aabbcc"
	full, err := resolveDownloadID(partial)
	if err != nil {
		t.Fatalf("Failed to resolve ID: %v", err)
	}

	if full != downloads[0].ID {
		t.Errorf("Expected %s, got %s", downloads[0].ID, full)
	}
}

// TestLsCmd_Alias verify 'l' alias exists
func TestLsCmd_Alias(t *testing.T) {
	found := false
	for _, alias := range lsCmd.Aliases {
		if alias == "l" {
			found = true
			break
		}
	}
	if !found {
		t.Error("lsCmd should have 'l' alias")
	}
}

// TestGetRemoteDownloads verify it parses response
func TestGetRemoteDownloads(t *testing.T) {
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"123","filename":"foo.bin","status":"downloading"}]`))
	}))
	defer server.Close()

	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	downloads, err := GetRemoteDownloads(port)
	if err != nil {
		t.Fatalf("Failed to get downloads: %v", err)
	}

	if len(downloads) != 1 {
		t.Fatalf("Expected 1 download, got %d", len(downloads))
	}
	if downloads[0].ID != "123" {
		t.Errorf("Mxpected ID 123, got %s", downloads[0].ID)
	}
}
