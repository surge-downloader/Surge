package core

import (
	"context"
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
