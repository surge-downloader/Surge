package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestLocalDownloadService_Delete_DBOnlyBroadcastsRemoved(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 20)
	pool := download.NewWorkerPool(ch, 1)
	svc := NewLocalDownloadServiceWithInput(pool, ch)
	defer func() { _ = svc.Shutdown() }()
	streamCh, cleanup, err := svc.StreamEvents(context.Background())
	if err != nil {
		t.Fatalf("failed to stream events: %v", err)
	}
	defer cleanup()

	id := "delete-db-only-id"
	url := "https://example.com/file.bin"
	destPath := filepath.Join(tempDir, "file.bin")
	incompletePath := destPath + types.IncompleteSuffix

	if err := os.WriteFile(incompletePath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("failed to create partial file: %v", err)
	}

	if err := state.SaveState(url, destPath, &types.DownloadState{
		ID:         id,
		URL:        url,
		DestPath:   destPath,
		Filename:   "file.bin",
		TotalSize:  1000,
		Downloaded: 200,
		Tasks: []types.Task{
			{Offset: 200, Length: 800},
		},
	}); err != nil {
		t.Fatalf("failed to seed state: %v", err)
	}

	if err := svc.Delete(id); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	gotRemoved := false
	deadline := time.After(500 * time.Millisecond)
	for !gotRemoved {
		select {
		case msg := <-streamCh:
			if m, ok := msg.(events.DownloadRemovedMsg); ok && m.DownloadID == id {
				gotRemoved = true
			}
		case <-deadline:
			t.Fatal("expected DownloadRemovedMsg for deleted DB-only download")
		}
	}

	if _, err := os.Stat(incompletePath); !os.IsNotExist(err) {
		t.Fatalf("expected partial file to be removed, stat err: %v", err)
	}

	entry, err := state.GetDownload(id)
	if err != nil {
		t.Fatalf("failed querying deleted entry: %v", err)
	}
	if entry != nil {
		t.Fatalf("expected entry to be removed, got %+v", entry)
	}
}

func TestLocalDownloadService_BatchProgress(t *testing.T) {
	// Start a local test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Probe request (HEAD or GET with Range: bytes=0-0)
		if r.Method == "HEAD" || r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Length", "1000")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusOK)
			return
		}

		// 2. Download request
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)

		// Send some data
		w.Write(make([]byte, 500))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Block to keep connection open so worker stays active
		time.Sleep(2 * time.Second)
	}))
	defer ts.Close()

	ch := make(chan interface{}, 20)
	// Create temporary directory for downloads
	tempDir := t.TempDir()

	pool := download.NewWorkerPool(ch, 1)
	svc := NewLocalDownloadServiceWithInput(pool, ch)
	defer func() { _ = svc.Shutdown() }()

	streamCh, cleanup, err := svc.StreamEvents(context.Background())
	if err != nil {
		t.Fatalf("failed to stream events: %v", err)
	}
	defer cleanup()

	// Add download using test server URL
	_, err = svc.Add(ts.URL, tempDir, "test-file", nil, nil)
	if err != nil {
		t.Fatalf("failed to add download: %v", err)
	}

	// Wait for a BatchProgressMsg
	// We need to wait enough time for the report loop to tick (150ms)
	deadline := time.After(2 * time.Second)
	gotBatch := false

	for !gotBatch {
		select {
		case msg := <-streamCh:
			if _, ok := msg.(events.BatchProgressMsg); ok {
				gotBatch = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for BatchProgressMsg")
		}
	}
}
