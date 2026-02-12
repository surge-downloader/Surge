package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
	"github.com/surge-downloader/surge/internal/utils"
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

func TestReadURLsFromFile_ParsesAndFilters(t *testing.T) {
	tmpDir := t.TempDir()
	urlFile := filepath.Join(tmpDir, "urls.txt")
	content := strings.Join([]string{
		"",
		"   # comment line",
		"https://example.com/a.zip",
		"  https://example.com/b.zip  ",
		"   ",
		"#another-comment",
		"https://example.com/c.zip",
	}, "\n")
	if err := os.WriteFile(urlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write url file: %v", err)
	}

	urls, err := readURLsFromFile(urlFile)
	if err != nil {
		t.Fatalf("readURLsFromFile returned error: %v", err)
	}

	want := []string{
		"https://example.com/a.zip",
		"https://example.com/b.zip",
		"https://example.com/c.zip",
	}
	if len(urls) != len(want) {
		t.Fatalf("expected %d urls, got %d (%v)", len(want), len(urls), urls)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Fatalf("url[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestReadURLsFromFile_MissingFile(t *testing.T) {
	_, err := readURLsFromFile(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected an error for missing file")
	}
}

func TestServerPIDLifecycle(t *testing.T) {
	setupIsolatedCmdState(t)
	removePID()

	savePID()
	pid := readPID()
	if pid != os.Getpid() {
		t.Fatalf("readPID() = %d, want current pid %d", pid, os.Getpid())
	}

	removePID()
	if got := readPID(); got != 0 {
		t.Fatalf("expected pid=0 after removePID, got %d", got)
	}
}

func TestFormatSize_Table(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 B"},
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "kb", bytes: 1024, want: "1.0 KB"},
		{name: "kb-fraction", bytes: 1536, want: "1.5 KB"},
		{name: "mb", bytes: 1024 * 1024, want: "1.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.ConvertBytesToHumanReadable(tt.bytes); got != tt.want {
				t.Fatalf("ConvertBytesToHumanReadable(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestPrintDownloadDetail_TextAndJSON(t *testing.T) {
	status := types.DownloadStatus{
		ID:         "aabbccdd-1234-5678-90ab-cdef12345678",
		URL:        "https://example.com/file.zip",
		Filename:   "file.zip",
		Status:     "downloading",
		Progress:   55.5,
		Downloaded: 1110,
		TotalSize:  2000,
		Speed:      2.5,
		Error:      "sample error",
	}

	textOut := captureStdout(t, func() {
		printDownloadDetail(status, false)
	})
	if !strings.Contains(textOut, "ID:         "+status.ID) {
		t.Fatalf("expected text output to contain ID, got: %s", textOut)
	}
	if !strings.Contains(textOut, "Speed:      2.5 MB/s") {
		t.Fatalf("expected text output to contain speed, got: %s", textOut)
	}
	if !strings.Contains(textOut, "Error:      sample error") {
		t.Fatalf("expected text output to contain error, got: %s", textOut)
	}

	jsonOut := captureStdout(t, func() {
		printDownloadDetail(status, true)
	})
	var decoded types.DownloadStatus
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("failed to decode JSON output: %v (out=%q)", err, jsonOut)
	}
	if decoded.ID != status.ID {
		t.Fatalf("decoded id = %q, want %q", decoded.ID, status.ID)
	}
}

func TestPrintDownloads_FromDatabase_TableAndJSON(t *testing.T) {
	setupIsolatedCmdState(t)
	removeActivePort()

	entry := types.DownloadEntry{
		ID:         "12345678-1234-1234-1234-1234567890ab",
		URL:        "https://example.com/asset.bin",
		Filename:   "this-is-a-very-long-file-name-that-should-truncate.bin",
		Status:     "downloading",
		Downloaded: 512,
		TotalSize:  1024,
	}
	if err := state.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed db entry: %v", err)
	}

	tableOut := captureStdout(t, func() {
		printDownloads(false)
	})
	if !strings.Contains(tableOut, "ID") {
		t.Fatalf("expected table header in output, got: %s", tableOut)
	}
	if !strings.Contains(tableOut, "12345678") {
		t.Fatalf("expected truncated ID in output, got: %s", tableOut)
	}
	if !strings.Contains(tableOut, "...") {
		t.Fatalf("expected truncated filename in output, got: %s", tableOut)
	}
	if !strings.Contains(tableOut, "50.0%") {
		t.Fatalf("expected computed progress in output, got: %s", tableOut)
	}

	jsonOut := captureStdout(t, func() {
		printDownloads(true)
	})
	var infos []downloadInfo
	if err := json.Unmarshal([]byte(jsonOut), &infos); err != nil {
		t.Fatalf("failed to decode json output: %v (out=%q)", err, jsonOut)
	}
	if len(infos) != 1 || infos[0].ID != entry.ID {
		t.Fatalf("unexpected JSON payload: %+v", infos)
	}
}

func TestPrintDownloads_JSONEmpty(t *testing.T) {
	setupIsolatedCmdState(t)
	removeActivePort()

	out := captureStdout(t, func() {
		printDownloads(true)
	})
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("expected empty json array, got %q", strings.TrimSpace(out))
	}
}

func TestShowDownloadDetails_UsesDatabaseFallback(t *testing.T) {
	setupIsolatedCmdState(t)
	removeActivePort()

	entry := types.DownloadEntry{
		ID:         "87654321-1234-1234-1234-1234567890ab",
		URL:        "https://example.com/detail.bin",
		Filename:   "detail.bin",
		Status:     "completed",
		Downloaded: 250,
		TotalSize:  500,
	}
	if err := state.AddToMasterList(entry); err != nil {
		t.Fatalf("failed to seed db entry: %v", err)
	}

	out := captureStdout(t, func() {
		showDownloadDetails("87654321", true)
	})

	var decoded types.DownloadStatus
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("failed to decode detail json: %v (out=%q)", err, out)
	}
	if decoded.ID != entry.ID {
		t.Fatalf("decoded id = %q, want %q", decoded.ID, entry.ID)
	}
	if decoded.Progress != 50 {
		t.Fatalf("decoded progress = %v, want 50", decoded.Progress)
	}
}

func TestSendToServer_SuccessAndServerError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{name: "success accepted", statusCode: http.StatusAccepted, body: `{"id":"abc"}`},
		{name: "server error", statusCode: http.StatusInternalServerError, body: "boom", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen failed: %v", err)
			}
			defer func() { _ = ln.Close() }()

			mux := http.NewServeMux()
			mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("expected POST, got %s", r.Method)
				}
				body, _ := io.ReadAll(r.Body)
				if !bytes.Contains(body, []byte(`"url":"https://example.com/file.zip"`)) {
					t.Fatalf("request body missing expected URL: %s", string(body))
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			})

			server := &http.Server{Handler: mux}
			go func() { _ = server.Serve(ln) }()
			t.Cleanup(func() {
				_ = server.Close()
			})

			port := ln.Addr().(*net.TCPAddr).Port
			err = sendToServer("https://example.com/file.zip", nil, "", port)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGetRemoteDownloads_NonOKAndInvalidJSON(t *testing.T) {
	t.Run("non-200", func(t *testing.T) {
		server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusUnauthorized)
		}))
		defer server.Close()

		_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)

		_, err := GetRemoteDownloads(port)
		if err == nil {
			t.Fatal("expected error for non-200 response")
		}
	})

	t.Run("invalid-json", func(t *testing.T) {
		server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{invalid"))
		}))
		defer server.Close()

		_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
		var port int
		_, _ = fmt.Sscanf(portStr, "%d", &port)

		_, err := GetRemoteDownloads(port)
		if err == nil {
			t.Fatal("expected json decode error")
		}
	})
}

func TestProcessDownloads_RemoteAndLocal(t *testing.T) {
	t.Run("remote-mode", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen failed: %v", err)
		}
		defer func() { _ = ln.Close() }()

		var received int32
		mux := http.NewServeMux()
		mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&received, 1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"ok"}`))
		})
		server := &http.Server{Handler: mux}
		go func() { _ = server.Serve(ln) }()
		t.Cleanup(func() { _ = server.Close() })

		port := ln.Addr().(*net.TCPAddr).Port
		count := processDownloads([]string{
			"https://example.com/a.zip,https://mirror.example.com/a.zip",
			"",
			"https://example.com/b.zip",
		}, "", port)

		if count != 2 {
			t.Fatalf("expected 2 successful remote adds, got %d", count)
		}
		if atomic.LoadInt32(&received) != 2 {
			t.Fatalf("expected 2 remote requests, got %d", received)
		}
	})

	t.Run("local-mode", func(t *testing.T) {
		setupIsolatedCmdState(t)
		atomic.StoreInt32(&activeDownloads, 0)

		GlobalProgressCh = make(chan any, 10)
		GlobalPool = download.NewWorkerPool(GlobalProgressCh, 2)
		GlobalService = core.NewLocalDownloadService(GlobalPool)

		count := processDownloads([]string{
			"https://example.com/local.zip",
			"",
		}, t.TempDir(), 0)

		if count != 1 {
			t.Fatalf("expected 1 successful local add, got %d", count)
		}
		if atomic.LoadInt32(&activeDownloads) != 1 {
			t.Fatalf("expected activeDownloads=1, got %d", atomic.LoadInt32(&activeDownloads))
		}
	})
}

func setupIsolatedCmdState(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("APPDATA", tempDir)
	t.Setenv("USERPROFILE", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HOME", tempDir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	state.CloseDB()
	state.Configure(filepath.Join(config.GetStateDir(), "surge.db"))
	t.Cleanup(func() {
		state.CloseDB()
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer failed: %v", err)
	}
	os.Stdout = old

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout failed: %v", err)
	}
	_ = r.Close()
	return string(data)
}
