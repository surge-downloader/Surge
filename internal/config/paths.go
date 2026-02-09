package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// [TODO]: clean the code here using os.UserConfigDir and os.UserCacheDir.
func GetSurgeDir() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "surge")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "surge")

	case "linux":
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			home, _ := os.UserHomeDir()
			configHome = filepath.Join(home, ".config")
		}
		return filepath.Join(configHome, "surge")
	default:
		configDir, _ := os.UserConfigDir()
		return filepath.Join(configDir, "surge")
	}
}

// Returns directory for state files
// [TODO]: Respect `XDG_DATA_HOME`
func GetDataDir() string {
	return filepath.Join(GetSurgeDir(), "data")
}

// Returns directory for logs
// [TODO]: There is no reason we need to implement our own logging solution. At least for linux.
// So we can delete the logging logic completely.
func GetLogsDir() string {
	return filepath.Join(GetSurgeDir(), "logs")
}

func GetRuntimeDir() string {
	var base string

	// XDG_RUNTIME_DIR or /run/user/<uid>
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			base = dir
		} else {
			uid := os.Getuid()
			if uid >= 0 {
				path := filepath.Join("/run/user", strconv.Itoa(uid))
				if stat, err := os.Stat(path); err == nil && stat.IsDir() {
					base = path
				}
			}
		}
	}

	// fallback for windows and macos
	if base == "" {
		base = os.TempDir()
	}

	runtimeDir := filepath.Join(base, "surge")

	if err := os.MkdirAll(runtimeDir, 0700); err != nil {
		panic(err)
	}

	return runtimeDir
}

// EnsureDirs creates all required directories
func EnsureDirs() error {
	dirs := []string{GetSurgeDir(), GetDataDir(), GetLogsDir(), GetRuntimeDir()}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
