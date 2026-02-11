package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/types"
)

var errTest = errors.New("test error")

func TestGenerateUniqueFilename(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "surge-tui-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Helper to create a dummy file
	createFile := func(name string) {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	tests := []struct {
		name               string
		existingFiles      []string
		activeDownload     string // filename of an active (non-done) download in the model
		activeDownloadDest string // destination path of an active download (tests Destination check)
		inputFilename      string
		want               string
	}{
		{
			name:          "No conflict",
			existingFiles: []string{},
			inputFilename: "file.txt",
			want:          "file.txt",
		},
		{
			name:          "Conflict with existing file",
			existingFiles: []string{"file.txt"},
			inputFilename: "file.txt",
			want:          "file(1).txt",
		},
		{
			name:          "Conflict with .surge file (paused download)",
			existingFiles: []string{"file.txt.surge"},
			inputFilename: "file.txt",
			want:          "file(1).txt",
		},
		{
			name:          "Conflict with both final and .surge file",
			existingFiles: []string{"file.txt", "file(1).txt.surge"},
			inputFilename: "file.txt",
			want:          "file(2).txt",
		},
		{
			name:          "Multiple .surge conflicts",
			existingFiles: []string{"1GB.bin.surge", "1GB(1).bin.surge"},
			inputFilename: "1GB.bin",
			want:          "1GB(2).bin",
		},
		{
			name:           "Conflict with active download in list",
			existingFiles:  []string{},
			activeDownload: "file.txt",
			inputFilename:  "file.txt",
			want:           "file(1).txt",
		},
		{
			name:           "Combined: file on disk and active download",
			existingFiles:  []string{"file.txt"},
			activeDownload: "file(1).txt",
			inputFilename:  "file.txt",
			want:           "file(2).txt",
		},
		{
			name:           "Combined: .surge file and active download",
			existingFiles:  []string{"file.txt.surge"},
			activeDownload: "file(1).txt",
			inputFilename:  "file.txt",
			want:           "file(2).txt",
		},
		{
			name:               "Conflict with download by Destination path",
			existingFiles:      []string{},
			activeDownloadDest: "/downloads/file.txt",
			inputFilename:      "file.txt",
			want:               "file(1).txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal RootModel
			m := &RootModel{
				downloads: []*DownloadModel{},
			}

			// Add active download if specified
			if tt.activeDownload != "" {
				m.downloads = append(m.downloads, &DownloadModel{
					Filename: tt.activeDownload,
					done:     false,
				})
			}

			// Add active download by destination path if specified
			if tt.activeDownloadDest != "" {
				m.downloads = append(m.downloads, &DownloadModel{
					Destination: tt.activeDownloadDest,
					done:        false,
				})
			}

			// Setup existing files
			for _, f := range tt.existingFiles {
				createFile(f)
			}
			// Cleanup after test case
			defer func() {
				for _, f := range tt.existingFiles {
					_ = os.Remove(filepath.Join(tmpDir, f))
				}
			}()

			got := m.generateUniqueFilename(tmpDir, tt.inputFilename)
			if got != tt.want {
				t.Errorf("generateUniqueFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdate_ResumeResultClearsFlags(t *testing.T) {
	m := RootModel{
		downloads: []*DownloadModel{
			{ID: "id-1", paused: true, pausing: true, pendingResume: true},
		},
	}

	updated, _ := m.Update(resumeResultMsg{id: "id-1", err: nil})
	m2 := updated.(RootModel)

	if len(m2.downloads) != 1 {
		t.Fatalf("Expected 1 download, got %d", len(m2.downloads))
	}
	d := m2.downloads[0]
	if d.paused || d.pausing || d.pendingResume {
		t.Fatalf("Expected flags cleared after resumeResultMsg success, got paused=%v pausing=%v pendingResume=%v", d.paused, d.pausing, d.pendingResume)
	}
}

func TestUpdate_ResumeResultErrorKeepsFlags(t *testing.T) {
	m := RootModel{
		downloads: []*DownloadModel{
			{ID: "id-1", paused: true, pausing: true, pendingResume: true},
		},
	}

	updated, _ := m.Update(resumeResultMsg{id: "id-1", err: errTest})
	m2 := updated.(RootModel)
	d := m2.downloads[0]
	if !d.paused || !d.pausing || !d.pendingResume {
		t.Fatalf("Expected flags unchanged on resumeResultMsg error, got paused=%v pausing=%v pendingResume=%v", d.paused, d.pausing, d.pendingResume)
	}
}

func TestUpdate_DownloadStartedClearsFlags(t *testing.T) {
	dm := NewDownloadModel("id-1", "http://example.com/file", "file", 0)
	dm.paused = true
	dm.pausing = true
	dm.pendingResume = true
	m := RootModel{
		downloads:   []*DownloadModel{dm},
		list:        NewDownloadList(80, 20),
		logViewport: viewport.New(40, 5),
	}

	msg := events.DownloadStartedMsg{
		DownloadID: "id-1",
		URL:        "http://example.com/file",
		Filename:   "file",
		Total:      100,
		DestPath:   "/tmp/file",
		State:      types.NewProgressState("id-1", 100),
	}

	updated, _ := m.Update(msg)
	m2 := updated.(RootModel)
	var d *DownloadModel
	for _, dl := range m2.downloads {
		if dl.ID == "id-1" {
			d = dl
			break
		}
	}
	if d == nil {
		t.Fatal("Expected download id-1 to exist")
	}
	if d.paused || d.pausing || d.pendingResume {
		t.Fatalf("Expected flags cleared on DownloadStartedMsg, got paused=%v pausing=%v pendingResume=%v", d.paused, d.pausing, d.pendingResume)
	}
}

func TestUpdate_PauseResumeEventsNormalizeFlags(t *testing.T) {
	m := RootModel{
		downloads: []*DownloadModel{
			{ID: "id-1", paused: false, pausing: true, pendingResume: true},
		},
		list:        NewDownloadList(80, 20),
		logViewport: viewport.New(40, 5),
	}

	updated, _ := m.Update(events.DownloadPausedMsg{
		DownloadID: "id-1",
		Filename:   "file",
		Downloaded: 50,
	})
	m2 := updated.(RootModel)
	d := m2.downloads[0]
	if !d.paused || d.pausing || d.pendingResume {
		t.Fatalf("Expected paused=true and others false after DownloadPausedMsg, got paused=%v pausing=%v pendingResume=%v", d.paused, d.pausing, d.pendingResume)
	}

	updated, _ = m2.Update(events.DownloadResumedMsg{
		DownloadID: "id-1",
		Filename:   "file",
	})
	m3 := updated.(RootModel)
	d = m3.downloads[0]
	if d.paused || d.pausing || d.pendingResume {
		t.Fatalf("Expected flags cleared after DownloadResumedMsg, got paused=%v pausing=%v pendingResume=%v", d.paused, d.pausing, d.pendingResume)
	}
}

func TestUpdate_DownloadRemovedRemovesFromModelAndList(t *testing.T) {
	dm := NewDownloadModel("id-1", "http://example.com/file", "file", 100)
	m := RootModel{
		downloads:   []*DownloadModel{dm},
		list:        NewDownloadList(80, 20),
		logViewport: viewport.New(40, 5),
	}
	m.UpdateListItems()

	updated, _ := m.Update(events.DownloadRemovedMsg{
		DownloadID: "id-1",
		Filename:   "file",
	})
	m2 := updated.(RootModel)

	if len(m2.downloads) != 0 {
		t.Fatalf("expected removed download to be absent, got %d entries", len(m2.downloads))
	}

	if len(m2.list.Items()) != 0 {
		t.Fatalf("expected list to be empty after removal, got %d items", len(m2.list.Items()))
	}
}

func TestUpdate_DownloadRemoved_NoOpWhenUnknownID(t *testing.T) {
	dm := NewDownloadModel("id-1", "http://example.com/file", "file", 100)
	m := RootModel{
		downloads:   []*DownloadModel{dm},
		list:        NewDownloadList(80, 20),
		logViewport: viewport.New(40, 5),
	}
	m.UpdateListItems()

	updated, _ := m.Update(events.DownloadRemovedMsg{
		DownloadID: "id-unknown",
		Filename:   "file",
	})
	m2 := updated.(RootModel)

	if len(m2.downloads) != 1 {
		t.Fatalf("expected unknown remove to keep entries, got %d", len(m2.downloads))
	}
}

func TestGenerateUniqueFilename_EmptyFilename(t *testing.T) {
	m := &RootModel{}
	got := m.generateUniqueFilename("/tmp", "")
	if got != "" {
		t.Errorf("generateUniqueFilename() with empty filename = %v, want empty string", got)
	}
}

func TestGenerateUniqueFilename_IncompleteSuffixConstant(t *testing.T) {
	// Verify the constant we're using is correct
	if types.IncompleteSuffix != ".surge" {
		t.Errorf("IncompleteSuffix = %q, want .surge", types.IncompleteSuffix)
	}
}

func TestUpdate_DownloadRequestMsg(t *testing.T) {
	// Setup initial model
	ch := make(chan any, 100)
	pool := download.NewWorkerPool(ch, 1)

	m := RootModel{
		Settings:    config.DefaultSettings(),
		Service:     core.NewLocalDownloadServiceWithInput(pool, ch),
		logViewport: viewport.New(40, 5),
		list:        NewDownloadList(40, 10),
	}

	// 1. Test Extension Prompt Enabled
	m.Settings.General.ExtensionPrompt = true
	m.Settings.General.WarnOnDuplicate = true

	msg := events.DownloadRequestMsg{
		URL:      "http://example.com/test.zip",
		Filename: "test.zip",
	}

	newM, _ := m.Update(msg)
	newRoot := newM.(RootModel)

	if newRoot.state != ExtensionConfirmationState {
		t.Errorf("Expected ExtensionConfirmationState, got %v", newRoot.state)
	}
	if newRoot.pendingURL != msg.URL {
		t.Errorf("Expected pendingURL=%s, got %s", msg.URL, newRoot.pendingURL)
	}

	// 2. Test Duplicate Warning (when prompt disabled but duplicate exists)
	m.Settings.General.ExtensionPrompt = false
	m.Settings.General.WarnOnDuplicate = true

	// Add existing download
	m.downloads = append(m.downloads, &DownloadModel{
		URL:      "http://example.com/test.zip",
		Filename: "test.zip",
	})

	newM, _ = m.Update(msg)
	newRoot = newM.(RootModel)

	if newRoot.state != DuplicateWarningState {
		t.Errorf("Expected DuplicateWarningState, got %v", newRoot.state)
	}

	// 3. Test No Prompt (Direct Download)
	m.Settings.General.ExtensionPrompt = false
	m.Settings.General.WarnOnDuplicate = true
	m.downloads = nil // Clear downloads

	// Note: startDownload triggers a command (tea.Cmd), and might update state or lists.
	// Since startDownload also does TUI side effects (addLogEntry), we might just check that
	// it DOESN'T enter a confirmation state.

	newM, _ = m.Update(msg)
	newRoot = newM.(RootModel)

	// Should remain in DashboardState (default) or whatever it was
	if newRoot.state == ExtensionConfirmationState || newRoot.state == DuplicateWarningState {
		t.Errorf("Expected no prompt state, got %v", newRoot.state)
	}
}
