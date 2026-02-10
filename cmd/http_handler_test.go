package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
)

func TestHandleDownload_PathResolution(t *testing.T) {
	// Setup temporary directory for mocking XDG_CONFIG_HOME
	tempDir, err := os.MkdirTemp("", "surge-test-home")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock XDG_CONFIG_HOME to affect GetSurgeDir() on Linux
	originalConfigHome := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tempDir)
	defer func() {
		if originalConfigHome == "" {
			_ = os.Unsetenv("XDG_CONFIG_HOME")
		} else {
			_ = os.Setenv("XDG_CONFIG_HOME", originalConfigHome)
		}
	}()

	// Create surge config directory
	surgeConfigDir := filepath.Join(tempDir, "surge")
	if err := os.MkdirAll(surgeConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Setup default download directory
	defaultDownloadDir := filepath.Join(tempDir, "Downloads")
	if err := os.MkdirAll(defaultDownloadDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a temporary settings file
	settings := config.DefaultSettings()
	settings.General.DefaultDownloadDir = defaultDownloadDir

	if err := config.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Initialize GlobalPool (required by handleDownload)
	GlobalPool = download.NewWorkerPool(nil, 1)

	tests := []struct {
		name                 string
		request              DownloadRequest
		expectedPathSuffix   string
		expectedPathAbsolute bool
	}{
		{
			name: "Absolute Path (Explicit)",
			request: DownloadRequest{
				URL:  "http://example.com/file1",
				Path: filepath.Join(tempDir, "absolute"),
			},
			expectedPathSuffix:   "absolute",
			expectedPathAbsolute: true,
		},
		{
			name: "Relative Path (No Flag)",
			request: DownloadRequest{
				URL:  "http://example.com/file2",
				Path: "relative",
			},
			expectedPathSuffix:   "relative",
			expectedPathAbsolute: true,
		},
		{
			name: "Relative to Default Dir",
			request: DownloadRequest{
				URL:                  "http://example.com/file3",
				Path:                 "subdir",
				RelativeToDefaultDir: true,
			},
			expectedPathSuffix:   filepath.Join(filepath.Base(defaultDownloadDir), "subdir"),
			expectedPathAbsolute: true,
		},
		{
			name: "Nested Relative to Default Dir",
			request: DownloadRequest{
				URL:                  "http://example.com/file4",
				Path:                 "nested/deep",
				RelativeToDefaultDir: true,
			},
			expectedPathSuffix:   filepath.Join(filepath.Base(defaultDownloadDir), "nested", "deep"),
			expectedPathAbsolute: true,
		},
		{
			name: "Empty Path (Default)",
			request: DownloadRequest{
				URL:  "http://example.com/file5",
				Path: "",
			},
			expectedPathSuffix:   filepath.Base(defaultDownloadDir), // Should be just the default dir
			expectedPathAbsolute: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest("POST", "/download", bytes.NewBuffer(body))
			w := httptest.NewRecorder()
			svc := core.NewLocalDownloadService(GlobalPool)

			// We pass defaultDownloadDir as a fallback to handleDownload, but since we mocked settings,
			// it should prioritize settings.General.DefaultDownloadDir
			handleDownload(w, req, defaultDownloadDir, svc)

			if w.Code != http.StatusOK && w.Code != http.StatusConflict {
				t.Errorf("Expected OK, got %d. Body: %s", w.Code, w.Body.String())
			}

			// GlobalPool access
			configs := GlobalPool.GetAll()
			found := false
			for _, cfg := range configs {
				if cfg.URL == tt.request.URL {
					found = true
					t.Logf("OutputPath for %s: %s", tt.name, cfg.OutputPath)

					if !filepath.IsAbs(cfg.OutputPath) && tt.expectedPathAbsolute {
						t.Errorf("Expected absolute path, got %s", cfg.OutputPath)
					}

					// Verify suffix
					// NOTE: cfg.OutputPath is absolute path.
					// If expectedPathSuffix is relative (like "relative"), we check if path ends with it.
					// If expectedPathSuffix is absolute (like /tmp/.../absolute), we check if paths match.

					if tt.request.RelativeToDefaultDir {
						expectedAbs := filepath.Join(defaultDownloadDir, tt.request.Path)
						if cfg.OutputPath != expectedAbs {
							t.Errorf("Expected path %s, got %s", expectedAbs, cfg.OutputPath)
						}
					} else if tt.request.Path == "" {
						if cfg.OutputPath != defaultDownloadDir {
							t.Errorf("Expected path %s, got %s", defaultDownloadDir, cfg.OutputPath)
						}
					} else if filepath.IsAbs(tt.request.Path) {
						if cfg.OutputPath != tt.request.Path {
							t.Errorf("Expected path %s, got %s", tt.request.Path, cfg.OutputPath)
						}
					} else {
						// Relative path without flag (Ensured Absolute CWD)
						// Hard to test exactly without knowing CWD, but it should end with the relative path
						if !strings.HasSuffix(cfg.OutputPath, tt.request.Path) {
							t.Errorf("Expected suffix %s, got %s", tt.request.Path, cfg.OutputPath)
						}
					}
					break
				}
			}
			if !found {
				t.Errorf("Download was not queued")
			}
		})
	}
}
