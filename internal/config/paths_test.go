package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetSurgeDir(t *testing.T) {
	// Set XDG_CONFIG_HOME for Linux tests
	if runtime.GOOS == "linux" {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
	}

	dir := GetSurgeDir()
	if dir == "" {
		t.Error("GetSurgeDir returned empty string")
	}
	// Should contain "surge" in path
	if !strings.Contains(strings.ToLower(dir), "surge") {
		t.Errorf("Expected path to contain 'surge', got: %s", dir)
	}
}

func TestGetStateDir(t *testing.T) {
	if runtime.GOOS == "linux" {
		tmpDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", tmpDir)

		dir := GetStateDir()
		expected := filepath.Join(tmpDir, "surge")
		if dir != expected {
			t.Errorf("GetStateDir mismatch. Got %s, want %s", dir, expected)
		}
	} else {
		// Non-linux: should be same as SurgeDir
		if GetStateDir() != GetSurgeDir() {
			t.Error("GetStateDir should equal GetSurgeDir on non-Linux")
		}
	}
}

func TestGetRuntimeDir(t *testing.T) {
	if runtime.GOOS == "linux" {
		// Case 1: XDG_RUNTIME_DIR set
		tmpDir := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", tmpDir)

		dir := GetRuntimeDir()
		expected := filepath.Join(tmpDir, "surge")
		if dir != expected {
			t.Errorf("GetRuntimeDir mismatch. Got %s, want %s", dir, expected)
		}

		// Case 2: XDG_RUNTIME_DIR unset (fallback)
		t.Setenv("XDG_RUNTIME_DIR", "")
		// Setup state dir for fallback check
		stateTmp := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateTmp)

		dirFallback := GetRuntimeDir()
		expectedFallback := filepath.Join(stateTmp, "surge")
		if dirFallback != expectedFallback {
			t.Errorf("GetRuntimeDir fallback mismatch. Got %s, want %s", dirFallback, expectedFallback)
		}
	}
}

func TestGetLogsDir(t *testing.T) {
	dir := GetLogsDir()
	if !strings.HasSuffix(dir, "logs") {
		t.Errorf("Expected path to end with 'logs', got: %s", dir)
	}

	// Should be under StateDir
	stateDir := GetStateDir()
	if !strings.HasPrefix(dir, stateDir) {
		t.Errorf("LogsDir should be under StateDir. LogsDir: %s, StateDir: %s", dir, stateDir)
	}
}

func TestEnsureDirs(t *testing.T) {
	// Setup temp env
	if runtime.GOOS == "linux" {
		baseDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(baseDir, "config"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(baseDir, "state"))
		t.Setenv("XDG_RUNTIME_DIR", filepath.Join(baseDir, "runtime"))
	}

	err := EnsureDirs()
	if err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	// Verify all directories exist
	dirs := []string{GetSurgeDir(), GetStateDir(), GetLogsDir(), GetRuntimeDir()}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if os.IsNotExist(err) {
			t.Errorf("Directory not created: %s", dir)
		} else if err != nil {
			t.Errorf("Error checking directory %s: %v", dir, err)
		} else if !info.IsDir() {
			t.Errorf("Path exists but is not a directory: %s", dir)
		}
	}
}

func TestDirectoryHierarchy(t *testing.T) {
	if runtime.GOOS != "linux" {
		surgeDir := GetSurgeDir()
		stateDir := GetStateDir()

		if stateDir != surgeDir {
			t.Errorf("On non-Linux, StateDir should be same as SurgeDir")
		}
	} else {
		// On Linux they should be distinct (assuming default different XDG vars)
		// We set them to ensure they are different
		t.Setenv("XDG_CONFIG_HOME", "/tmp/config")
		t.Setenv("XDG_STATE_HOME", "/tmp/state")

		if GetSurgeDir() == GetStateDir() {
			t.Error("On Linux, SurgeDir and StateDir should be different")
		}
	}
}

// TestMigrateOldPaths verifies that files are moved from config dir to state dir
func TestMigrateOldPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Migration only runs on Linux")
	}

	// Setup mock config/state dirs
	tmpDir := t.TempDir()
	mockConfig := filepath.Join(tmpDir, "config")
	mockState := filepath.Join(tmpDir, "state")

	t.Setenv("XDG_CONFIG_HOME", mockConfig)
	t.Setenv("XDG_STATE_HOME", mockState)

	// Create old layout
	oldSurgeDir := filepath.Join(mockConfig, "surge")
	if err := os.MkdirAll(oldSurgeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Create file to move: surge.db
	dbPath := filepath.Join(oldSurgeDir, "surge.db")
	if err := os.WriteFile(dbPath, []byte("fake db"), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Create logs dir to move
	logsDir := filepath.Join(oldSurgeDir, "logs")
	if err := os.Mkdir(logsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "test.log"), []byte("log data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run migration
	if err := MigrateOldPaths(); err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify surge.db moved
	newDbPath := filepath.Join(mockState, "surge", "surge.db")
	if _, err := os.Stat(newDbPath); os.IsNotExist(err) {
		t.Error("surge.db was not migrated to state dir")
	}
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("Old surge.db still exists")
	}

	// Verify logs moved
	newLogsDir := filepath.Join(mockState, "surge", "logs")
	if _, err := os.Stat(newLogsDir); os.IsNotExist(err) {
		t.Error("logs dir was not migrated")
	}
	if _, err := os.Stat(filepath.Join(newLogsDir, "test.log")); os.IsNotExist(err) {
		t.Error("log file missing in new location")
	}
	if _, err := os.Stat(logsDir); err == nil {
		t.Error("Old logs dir still exists")
	}
}
