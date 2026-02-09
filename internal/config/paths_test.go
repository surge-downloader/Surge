package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSurgeDir(t *testing.T) {
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
	dir := GetDataDir()
	if dir == "" {
		t.Error("GetStateDir returned empty string")
	}
	if !strings.HasSuffix(dir, "state") {
		t.Errorf("Expected path to end with 'state', got: %s", dir)
	}
	// State dir should be under surge dir
	surgeDir := GetSurgeDir()
	if !strings.HasPrefix(dir, surgeDir) {
		t.Errorf("StateDir should be under SurgeDir. StateDir: %s, SurgeDir: %s", dir, surgeDir)
	}
}

func TestGetLogsDir(t *testing.T) {
	dir := GetLogsDir()
	if dir == "" {
		t.Error("GetLogsDir returned empty string")
	}
	if !strings.HasSuffix(dir, "logs") {
		t.Errorf("Expected path to end with 'logs', got: %s", dir)
	}
	// Logs dir should be under surge dir
	surgeDir := GetSurgeDir()
	if !strings.HasPrefix(dir, surgeDir) {
		t.Errorf("LogsDir should be under SurgeDir. LogsDir: %s, SurgeDir: %s", dir, surgeDir)
	}
}

func TestEnsureDirs(t *testing.T) {
	err := EnsureDirs()
	if err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	// Verify all directories exist
	dirs := []string{GetSurgeDir(), GetDataDir(), GetLogsDir()}
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

// Extended tests for cross-platform path handling

func TestGetSurgeDir_AbsolutePath(t *testing.T) {
	dir := GetSurgeDir()
	if !filepath.IsAbs(dir) {
		t.Errorf("GetSurgeDir should return absolute path, got: %s", dir)
	}
}

func TestGetStateDir_AbsolutePath(t *testing.T) {
	dir := GetDataDir()
	if !filepath.IsAbs(dir) {
		t.Errorf("GetStateDir should return absolute path, got: %s", dir)
	}
}

func TestGetLogsDir_AbsolutePath(t *testing.T) {
	dir := GetLogsDir()
	if !filepath.IsAbs(dir) {
		t.Errorf("GetLogsDir should return absolute path, got: %s", dir)
	}
}

func TestPathConsistency(t *testing.T) {
	// Multiple calls should return the same paths
	dir1 := GetSurgeDir()
	dir2 := GetSurgeDir()
	if dir1 != dir2 {
		t.Errorf("GetSurgeDir should return consistent paths: %s vs %s", dir1, dir2)
	}

	state1 := GetDataDir()
	state2 := GetDataDir()
	if state1 != state2 {
		t.Errorf("GetStateDir should return consistent paths: %s vs %s", state1, state2)
	}

	logs1 := GetLogsDir()
	logs2 := GetLogsDir()
	if logs1 != logs2 {
		t.Errorf("GetLogsDir should return consistent paths: %s vs %s", logs1, logs2)
	}
}

func TestDirectoryHierarchy(t *testing.T) {
	surgeDir := GetSurgeDir()
	stateDir := GetDataDir()
	logsDir := GetLogsDir()

	// State and logs should be subdirectories of surge dir
	expectedStateDir := filepath.Join(surgeDir, "state")
	expectedLogsDir := filepath.Join(surgeDir, "logs")

	if stateDir != expectedStateDir {
		t.Errorf("StateDir should be %s, got: %s", expectedStateDir, stateDir)
	}

	if logsDir != expectedLogsDir {
		t.Errorf("LogsDir should be %s, got: %s", expectedLogsDir, logsDir)
	}
}

func TestEnsureDirs_Idempotent(t *testing.T) {
	// EnsureDirs should be safe to call multiple times
	for i := range 3 {
		err := EnsureDirs()
		if err != nil {
			t.Errorf("EnsureDirs failed on call %d: %v", i+1, err)
		}
	}
}

func TestPathsNoTrailingSlash(t *testing.T) {
	dirs := []struct {
		name string
		path string
	}{
		{"SurgeDir", GetSurgeDir()},
		{"StateDir", GetDataDir()},
		{"LogsDir", GetLogsDir()},
	}

	for _, d := range dirs {
		if strings.HasSuffix(d.path, string(filepath.Separator)) {
			t.Errorf("%s should not have trailing separator: %s", d.name, d.path)
		}
	}
}
