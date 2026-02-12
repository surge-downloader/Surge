package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

type mockWorkerPool struct {
	pauseReturn  bool
	resumeReturn bool

	added    []types.DownloadConfig
	canceled []string
	statuses map[string]*types.DownloadStatus
}

func (m *mockWorkerPool) Add(cfg types.DownloadConfig) {
	m.added = append(m.added, cfg)
}

func (m *mockWorkerPool) Pause(downloadID string) bool {
	return m.pauseReturn
}

func (m *mockWorkerPool) Resume(downloadID string) bool {
	return m.resumeReturn
}

func (m *mockWorkerPool) Cancel(downloadID string) {
	m.canceled = append(m.canceled, downloadID)
}

func (m *mockWorkerPool) GetAll() []types.DownloadConfig {
	return nil
}

func (m *mockWorkerPool) GetStatus(id string) *types.DownloadStatus {
	if m.statuses == nil {
		return nil
	}
	return m.statuses[id]
}

func (m *mockWorkerPool) GracefulShutdown() {}

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

func TestLocalDownloadService_Add_UsesConfiguredDefaultPath(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 20)
	mockPool := &mockWorkerPool{}
	svc := NewLocalDownloadServiceWithInput(nil, ch)
	svc.pool = mockPool
	defer func() { _ = svc.Shutdown() }()

	settings := config.DefaultSettings()
	settings.General.DefaultDownloadDir = tempDir

	svc.settingsMu.Lock()
	svc.settings = settings
	svc.settingsMu.Unlock()

	id, err := svc.Add("https://example.com/a.bin", "", "a.bin", nil, nil)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	if len(mockPool.added) != 1 {
		t.Fatalf("expected one added config, got %d", len(mockPool.added))
	}
	cfg := mockPool.added[0]
	if cfg.ID != id {
		t.Fatalf("config id mismatch: %s vs %s", cfg.ID, id)
	}
	if cfg.OutputPath != tempDir {
		t.Fatalf("unexpected output path: %s", cfg.OutputPath)
	}
	if cfg.State == nil || cfg.State.DestPath != filepath.Join(tempDir, "a.bin") {
		t.Fatalf("unexpected state dest path: %+v", cfg.State)
	}
}

func TestLocalDownloadService_Pause_DBFallbackBroadcastsPaused(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 20)
	svc := NewLocalDownloadServiceWithInput(nil, ch)
	svc.pool = &mockWorkerPool{pauseReturn: false}
	defer func() { _ = svc.Shutdown() }()

	streamCh, cleanup, err := svc.StreamEvents(context.Background())
	if err != nil {
		t.Fatalf("failed to stream events: %v", err)
	}
	defer cleanup()

	id := "pause-db-id"
	url := "https://example.com/p.bin"
	destPath := filepath.Join(tempDir, "p.bin")
	if err := state.SaveState(url, destPath, &types.DownloadState{
		ID:         id,
		URL:        url,
		DestPath:   destPath,
		Filename:   "p.bin",
		TotalSize:  1000,
		Downloaded: 250,
		Tasks:      []types.Task{{Offset: 250, Length: 750}},
	}); err != nil {
		t.Fatalf("failed to seed paused state: %v", err)
	}

	if err := svc.Pause(id); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	select {
	case msg := <-streamCh:
		paused, ok := msg.(events.DownloadPausedMsg)
		if !ok {
			t.Fatalf("expected DownloadPausedMsg, got %T", msg)
		}
		if paused.DownloadID != id || paused.Downloaded != 250 {
			t.Fatalf("unexpected paused message: %+v", paused)
		}
	case <-time.After(800 * time.Millisecond):
		t.Fatal("expected paused event")
	}
}

func TestLocalDownloadService_Resume_ColdResumeFromState(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 20)
	mockPool := &mockWorkerPool{resumeReturn: false}
	svc := NewLocalDownloadServiceWithInput(nil, ch)
	svc.pool = mockPool
	defer func() { _ = svc.Shutdown() }()

	settings := config.DefaultSettings()
	settings.General.DefaultDownloadDir = tempDir
	svc.settingsMu.Lock()
	svc.settings = settings
	svc.settingsMu.Unlock()

	streamCh, cleanup, err := svc.StreamEvents(context.Background())
	if err != nil {
		t.Fatalf("failed to stream events: %v", err)
	}
	defer cleanup()

	id := "resume-db-id"
	url := "https://example.com/r.bin"
	destPath := filepath.Join(tempDir, "r.bin")
	if err := state.SaveState(url, destPath, &types.DownloadState{
		ID:         id,
		URL:        url,
		DestPath:   destPath,
		Filename:   "r.bin",
		TotalSize:  2000,
		Downloaded: 800,
		Elapsed:    int64(3 * time.Second),
		Mirrors:    []string{"https://m1.example.com/r.bin"},
		Tasks:      []types.Task{{Offset: 800, Length: 1200}},
	}); err != nil {
		t.Fatalf("failed to seed resume state: %v", err)
	}

	if err := svc.Resume(id); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	select {
	case msg := <-streamCh:
		resumed, ok := msg.(events.DownloadResumedMsg)
		if !ok {
			t.Fatalf("expected DownloadResumedMsg, got %T", msg)
		}
		if resumed.DownloadID != id {
			t.Fatalf("unexpected resumed id: %s", resumed.DownloadID)
		}
	case <-time.After(800 * time.Millisecond):
		t.Fatal("expected resumed event")
	}

	if len(mockPool.added) != 1 {
		t.Fatalf("expected one pool add on cold resume, got %d", len(mockPool.added))
	}
	if !mockPool.added[0].IsResume {
		t.Fatalf("expected resumed config to be marked IsResume")
	}
}

func TestLocalDownloadService_List_DBStatuses(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	svc := NewLocalDownloadServiceWithInput(nil, make(chan interface{}, 1))
	defer func() { _ = svc.Shutdown() }()

	if err := state.AddToMasterList(types.DownloadEntry{
		ID:         "paused-1",
		URL:        "https://example.com/paused.bin",
		DestPath:   filepath.Join(tempDir, "paused.bin"),
		Filename:   "paused.bin",
		Status:     "paused",
		TotalSize:  1000,
		Downloaded: 400,
	}); err != nil {
		t.Fatalf("failed to seed paused entry: %v", err)
	}

	if err := state.AddToMasterList(types.DownloadEntry{
		ID:         "completed-1",
		URL:        "https://example.com/completed.bin",
		DestPath:   filepath.Join(tempDir, "completed.bin"),
		Filename:   "completed.bin",
		Status:     "completed",
		TotalSize:  1024 * 1024,
		Downloaded: 1024 * 1024,
		TimeTaken:  1000,
	}); err != nil {
		t.Fatalf("failed to seed completed entry: %v", err)
	}

	statuses, err := svc.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(statuses) < 2 {
		t.Fatalf("expected at least 2 statuses, got %d", len(statuses))
	}

	foundPaused := false
	foundCompleted := false
	for _, st := range statuses {
		switch st.ID {
		case "paused-1":
			foundPaused = true
			if st.Progress < 39.9 || st.Progress > 40.1 {
				t.Fatalf("unexpected paused progress: %f", st.Progress)
			}
		case "completed-1":
			foundCompleted = true
			if st.Progress != 100 {
				t.Fatalf("expected completed progress 100, got %f", st.Progress)
			}
			if st.Speed <= 0 {
				t.Fatalf("expected positive completed speed, got %f", st.Speed)
			}
		}
	}
	if !foundPaused || !foundCompleted {
		t.Fatalf("missing expected statuses, got ids: %v", func() []string {
			ids := make([]string, 0, len(statuses))
			for _, st := range statuses {
				ids = append(ids, st.ID)
			}
			return ids
		}())
	}
}

func TestLocalDownloadService_Resume_CompletedReturnsError(t *testing.T) {
	tempDir := t.TempDir()
	state.CloseDB()
	state.Configure(filepath.Join(tempDir, "surge.db"))
	defer state.CloseDB()

	ch := make(chan interface{}, 10)
	svc := NewLocalDownloadServiceWithInput(nil, ch)
	svc.pool = &mockWorkerPool{resumeReturn: false}
	defer func() { _ = svc.Shutdown() }()

	if err := state.AddToMasterList(types.DownloadEntry{
		ID:         "done-1",
		URL:        "https://example.com/done.bin",
		DestPath:   filepath.Join(tempDir, "done.bin"),
		Filename:   "done.bin",
		Status:     "completed",
		TotalSize:  100,
		Downloaded: 100,
	}); err != nil {
		t.Fatalf("failed to seed completed entry: %v", err)
	}

	err := svc.Resume("done-1")
	if err == nil || !strings.Contains(err.Error(), "already completed") {
		t.Fatalf("expected completed error, got: %v", err)
	}
}
