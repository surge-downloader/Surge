package config

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/surge-downloader/surge/internal/utils"
)

// GetSurgeDir returns the directory for configuration files (settings.json).
// Linux: $XDG_CONFIG_HOME/surge or ~/.config/surge
// macOS: ~/Library/Application Support/surge
// Windows: %APPDATA%/surge
func GetSurgeDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "surge")
	case "darwin": // MacOS
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "surge")
	default: // Linux
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			home, _ := os.UserHomeDir()
			configHome = filepath.Join(home, ".config")
		}
		return filepath.Join(configHome, "surge")
	}
}

// GetStateDir returns the directory for persistent state files (db, logs, token).
// Linux: $XDG_STATE_HOME/surge or ~/.local/state/surge
// macOS/Windows: matches GetSurgeDir()
func GetStateDir() string {
	if runtime.GOOS != "linux" {
		// On non-Linux, we keep everything in the main app directory (SurgeDir)
		return GetSurgeDir()
	}

	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, _ := os.UserHomeDir()
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "surge")
}

// GetRuntimeDir returns the directory for runtime files (pid, port, lock).
// Linux: $XDG_RUNTIME_DIR/surge or fallback to GetStateDir() if unset
// macOS: $TMPDIR/surge-runtime
// Windows: %TEMP%/surge
func GetRuntimeDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.TempDir(), "surge")
	case "darwin":
		return filepath.Join(os.TempDir(), "surge-runtime")
	default: // Linux
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir != "" {
			return filepath.Join(runtimeDir, "surge")
		}
		// Fallback to state dir if XDG_RUNTIME_DIR is not set (e.g. docker, headless)
		return GetStateDir()
	}
}

// GetLogsDir returns the directory for logs
func GetLogsDir() string {
	return filepath.Join(GetStateDir(), "logs")
}

// EnsureDirs creates all required directories
func EnsureDirs() error {
	dirs := []string{GetSurgeDir(), GetStateDir(), GetRuntimeDir(), GetLogsDir()}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// On Linux runtime dir, we might want stricter permissions (0700) if it's in /run/user
	if runtime.GOOS == "linux" && os.Getenv("XDG_RUNTIME_DIR") != "" {
		_ = os.Chmod(GetRuntimeDir(), 0o700)
	}

	return nil
}

// MigrateOldPaths moves files from the old flat layout (pre-XDG) to new locations.
// Only relevant on Linux.
func MigrateOldPaths() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	configDir := GetSurgeDir()
	stateDir := GetStateDir()

	// If config dir doesn't exist, nothing to migrate (fresh install)
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return nil
	}

	// Make sure target directories exist
	if err := EnsureDirs(); err != nil {
		return err
	}

	// Files to move from ConfigDir -> StateDir
	// Old location: ~/.config/surge/<file>
	// New location: ~/.local/state/surge/<file>
	filesToMove := []string{
		"surge.db",
		"token",
	}

	for _, filename := range filesToMove {
		oldPath := filepath.Join(configDir, filename)
		newPath := filepath.Join(stateDir, filename)

		// Move if old exists and new doesn't
		if _, err := os.Stat(oldPath); err == nil {
			if _, err := os.Stat(newPath); os.IsNotExist(err) {
				if err := os.Rename(oldPath, newPath); err != nil {
					utils.Debug("Failed to migrate %s from %s to %s: %v", filename, oldPath, newPath, err)
				} else {
					utils.Debug("Migrated %s to %s", filename, newPath)
				}
			} else {
				// Never delete potentially newer data (e.g. surge.db) when both
				// locations contain a file. Token now always lives in state dir:
				// delete legacy token to avoid ambiguity.
				if filename == "token" {
					if err := os.Remove(oldPath); err != nil {
						utils.Debug("Failed to remove legacy token file %s: %v", oldPath, err)
					} else {
						utils.Debug("Removed legacy token file %s (state token retained at %s)", oldPath, newPath)
					}
					continue
				}
				utils.Debug("Skipped migrating %s: both old and new files exist (%s, %s)", filename, oldPath, newPath)
			}
		}
	}

	// Directories to move
	// Old: ~/.config/surge/state -> ~/.local/state/surge (contents merged if needed)
	// Old: ~/.config/surge/logs -> ~/.local/state/surge/logs

	// 1. Move 'logs' dir contents
	oldLogs := filepath.Join(configDir, "logs")
	newLogs := filepath.Join(stateDir, "logs")

	if info, err := os.Stat(oldLogs); err == nil && info.IsDir() {
		// Ensure new logs dir exists
		if err := os.MkdirAll(newLogs, 0755); err != nil {
			return err
		}

		entries, _ := os.ReadDir(oldLogs)
		for _, entry := range entries {
			oldPath := filepath.Join(oldLogs, entry.Name())
			newPath := filepath.Join(newLogs, entry.Name())
			if err := os.Rename(oldPath, newPath); err != nil {
				utils.Debug("Failed to migrate log file %s to %s: %v", oldPath, newPath, err)
			}
		}
		_ = os.Remove(oldLogs)
	}

	// 2. Handle old 'state' subdir if it exists (some previous versions might have used it)
	// In the previous codebase check, GetStateDir() was GetSurgeDir()/state
	// So we need to move contents of ~/.config/surge/state/* to ~/.local/state/surge/*
	oldStateDir := filepath.Join(configDir, "state")
	if info, err := os.Stat(oldStateDir); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(oldStateDir)
		for _, entry := range entries {
			oldPath := filepath.Join(oldStateDir, entry.Name())
			newPath := filepath.Join(stateDir, entry.Name())
			if err := os.Rename(oldPath, newPath); err != nil {
				utils.Debug("Failed to migrate state file %s to %s: %v", oldPath, newPath, err)
			}
		}
		// Remove empty old state dir
		_ = os.Remove(oldStateDir)
	}

	return nil
}
