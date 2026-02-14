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
	"github.com/surge-downloader/surge/internal/testutil"
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

func TestLocalDownloadService_Delete_ActiveWithoutDB_RemovesPartialFile(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 100)
	pool := download.NewWorkerPool(ch, 1)
	svc := NewLocalDownloadServiceWithInput(pool, ch)
	defer func() { _ = svc.Shutdown() }()

	server := testutil.NewStreamingMockServerT(t,
		200*1024*1024,
		testutil.WithRangeSupport(true),
		testutil.WithLatency(8*time.Millisecond),
	)
	defer server.Close()

	outputDir := t.TempDir()
	const filename = "active-delete.bin"
	id, err := svc.Add(server.URL(), outputDir, filename, nil, nil)
	if err != nil {
		t.Fatalf("failed to add download: %v", err)
	}

	destPath := filepath.Join(outputDir, filename)
	incompletePath := destPath + types.IncompleteSuffix

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(incompletePath); err == nil {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if _, err := os.Stat(incompletePath); err != nil {
		t.Fatalf("expected partial file to exist before delete, stat err: %v", err)
	}

	// Simulate delete-before-persist path: no DB entry available.
	_ = state.RemoveFromMasterList(id)

	if err := svc.Delete(id); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(incompletePath); os.IsNotExist(err) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(incompletePath); !os.IsNotExist(err) {
		t.Fatalf("expected partial file to be removed, stat err: %v", err)
	}
}

func TestLocalDownloadService_Shutdown_Idempotent(t *testing.T) {
	ch := make(chan interface{}, 1)
	svc := NewLocalDownloadServiceWithInput(nil, ch)

	if err := svc.Shutdown(); err != nil {
		t.Fatalf("first shutdown failed: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected input channel to be closed after shutdown")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for input channel to close")
	}

	if err := svc.Shutdown(); err != nil {
		t.Fatalf("second shutdown failed: %v", err)
	}
}

func TestLocalDownloadService_Shutdown_PersistsPausedState(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 100)
	pool := download.NewWorkerPool(ch, 1)
	svc := NewLocalDownloadServiceWithInput(pool, ch)
	defer func() { _ = svc.Shutdown() }()

	server := testutil.NewStreamingMockServerT(t,
		500*1024*1024,
		testutil.WithRangeSupport(true),
		testutil.WithLatency(10*time.Millisecond),
	)
	defer server.Close()

	outputDir := t.TempDir()
	const filename = "persist.bin"
	id, err := svc.Add(server.URL(), outputDir, filename, nil, nil)
	if err != nil {
		t.Fatalf("failed to add download: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	progressed := false
	for time.Now().Before(deadline) {
		st, err := svc.GetStatus(id)
		if err == nil && st != nil && st.Downloaded > 0 {
			progressed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !progressed {
		t.Fatal("download did not make progress before shutdown")
	}

	if err := svc.Shutdown(); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	entry, err := state.GetDownload(id)
	if err != nil {
		t.Fatalf("failed to fetch persisted download: %v", err)
	}
	if entry == nil {
		t.Fatal("expected persisted download entry after shutdown")
	}
	if entry.Status != "paused" {
		t.Fatalf("status = %q, want paused", entry.Status)
	}
	if entry.Downloaded == 0 {
		t.Fatal("expected persisted paused download to have non-zero progress")
	}

	statuses, err := svc.List()
	if err != nil {
		t.Fatalf("failed to list downloads after shutdown: %v", err)
	}
	foundInList := false
	for _, st := range statuses {
		if st.ID == id {
			foundInList = true
			if st.Status != "paused" && st.Status != "pausing" {
				t.Fatalf("list status = %q, want paused/pausing", st.Status)
			}
			break
		}
	}
	if !foundInList {
		t.Fatal("expected paused download to remain visible in list after shutdown")
	}

	destPath := filepath.Join(outputDir, filename)
	saved, err := state.LoadState(server.URL(), destPath)
	if err != nil {
		t.Fatalf("failed to load saved state: %v", err)
	}
	if saved.ID != id {
		t.Fatalf("saved state id = %q, want %q", saved.ID, id)
	}
	if len(saved.Tasks) == 0 {
		t.Fatal("expected saved state to include remaining tasks")
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
		if _, err := w.Write(make([]byte, 500)); err != nil {
			t.Errorf("failed to write data: %v", err)
		}
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

func TestLocalDownloadService_ResumeRejectedWhilePausing(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 100)
	pool := download.NewWorkerPool(ch, 1)
	svc := NewLocalDownloadServiceWithInput(pool, ch)
	defer func() { _ = svc.Shutdown() }()

	server := testutil.NewStreamingMockServerT(t,
		500*1024*1024,
		testutil.WithRangeSupport(true),
		testutil.WithLatency(10*time.Millisecond),
	)
	defer server.Close()

	outputDir := t.TempDir()
	id, err := svc.Add(server.URL(), outputDir, "resume-race.bin", nil, nil)
	if err != nil {
		t.Fatalf("failed to add download: %v", err)
	}

	// Wait until download starts moving.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := svc.GetStatus(id)
		if st != nil && st.Downloaded > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := svc.Pause(id); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	// If pause finalized too fast on this machine, skip this race-specific assertion.
	st, _ := svc.GetStatus(id)
	if st == nil || st.Status != "pausing" {
		t.Skip("download transitioned out of pausing before resume-race assertion")
	}

	if err := svc.Resume(id); err == nil {
		t.Fatal("expected resume to fail while download is still pausing")
	}
}
