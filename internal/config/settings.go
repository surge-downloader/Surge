package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Settings holds all user-configurable application settings organized by category.
type Settings struct {
	General     GeneralSettings     `json:"general"`
	Network     NetworkSettings     `json:"network"`
	Performance PerformanceSettings `json:"performance"`
}

// GeneralSettings contains application behavior settings.
type GeneralSettings struct {
	DefaultDownloadDir string `json:"default_download_dir"`
	WarnOnDuplicate    bool   `json:"warn_on_duplicate"`
	ExtensionPrompt    bool   `json:"extension_prompt"`
	AutoResume         bool   `json:"auto_resume"`
	SkipUpdateCheck    bool   `json:"skip_update_check"`

	ClipboardMonitor  bool `json:"clipboard_monitor"`
	Theme             int  `json:"theme"`
	LogRetentionCount int  `json:"log_retention_count"`
}

const (
	ThemeAdaptive = 0
	ThemeLight    = 1
	ThemeDark     = 2
)

// NetworkSettings contains network connection parameters.
type NetworkSettings struct {
	MaxConnectionsPerHost  int    `json:"max_connections_per_host"`
	MaxConcurrentDownloads int    `json:"max_concurrent_downloads"`
	UserAgent              string `json:"user_agent"`
	ProxyURL               string `json:"proxy_url"`
	SequentialDownload     bool   `json:"sequential_download"`
	MinChunkSize           int64  `json:"min_chunk_size"`
	WorkerBufferSize       int    `json:"worker_buffer_size"`
}

// UnmarshalJSON implements custom JSON unmarshalling for Settings.
// This provides backward compatibility with the legacy "connections" + "chunks"
// migrating them into the new unified "network" field.
func (s *Settings) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion (alias has no methods)
	type Alias Settings
	if err := json.Unmarshal(data, (*Alias)(s)); err != nil {
		return err
	}

	// Check if the JSON had legacy keys instead of "network"
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil // Already parsed above, ignore raw parse errors
	}

	if _, hasNetwork := raw["network"]; !hasNetwork {
		// Migrate legacy "connections" key (overlays onto Network)
		if conn, ok := raw["connections"]; ok {
			_ = json.Unmarshal(conn, &s.Network)
		}
		// Migrate legacy "chunks" key (overlays chunk fields onto Network)
		if chunks, ok := raw["chunks"]; ok {
			_ = json.Unmarshal(chunks, &s.Network)
		}
	}

	return nil
}

// PerformanceSettings contains performance tuning parameters.
type PerformanceSettings struct {
	MaxTaskRetries        int           `json:"max_task_retries"`
	SlowWorkerThreshold   float64       `json:"slow_worker_threshold"`
	SlowWorkerGracePeriod time.Duration `json:"slow_worker_grace_period"`
	StallTimeout          time.Duration `json:"stall_timeout"`
	SpeedEmaAlpha         float64       `json:"speed_ema_alpha"`
}

// SettingMeta provides metadata for a single setting (for UI rendering).
type SettingMeta struct {
	Key         string // JSON key name
	Label       string // Human-readable label
	Description string // Help text displayed in right pane
	Type        string // "string", "int", "int64", "bool", "duration", "float64"
}

// GetSettingsMetadata returns metadata for all settings organized by category.
func GetSettingsMetadata() map[string][]SettingMeta {
	return map[string][]SettingMeta{
		"General": {
			{Key: "default_download_dir", Label: "Default Download Dir", Description: "Default directory for new downloads. Leave empty to use current directory.", Type: "string"},
			{Key: "warn_on_duplicate", Label: "Warn on Duplicate", Description: "Show warning when adding a download that already exists.", Type: "bool"},
			{Key: "extension_prompt", Label: "Extension Prompt", Description: "Prompt for confirmation when adding downloads via browser extension.", Type: "bool"},
			{Key: "auto_resume", Label: "Auto Resume", Description: "Automatically resume paused downloads on startup.", Type: "bool"},
			{Key: "skip_update_check", Label: "Skip Update Check", Description: "Disable automatic check for new versions on startup.", Type: "bool"},

			{Key: "clipboard_monitor", Label: "Clipboard Monitor", Description: "Watch clipboard for URLs and prompt to download them.", Type: "bool"},
			{Key: "theme", Label: "App Theme", Description: "UI Theme (System, Light, Dark).", Type: "int"},
			{Key: "log_retention_count", Label: "Log Retention Count", Description: "Number of recent log files to keep.", Type: "int"},
		},
		"Network": {
			{Key: "max_connections_per_host", Label: "Max Connections/Host", Description: "Maximum concurrent connections per host (1-64).", Type: "int"},
			{Key: "max_concurrent_downloads", Label: "Max Concurrent Downloads", Description: "Maximum number of downloads running at once (1-10). Requires restart.", Type: "int"},
			{Key: "user_agent", Label: "User Agent", Description: "Custom User-Agent string for HTTP requests. Leave empty for default.", Type: "string"},
			{Key: "proxy_url", Label: "Proxy URL", Description: "HTTP/HTTPS proxy URL (e.g. http://127.0.0.1:1700). Leave empty to use system default.", Type: "string"},
			{Key: "sequential_download", Label: "Sequential Download", Description: "Download pieces in order (Streaming Mode). May be slower.", Type: "bool"},
			{Key: "min_chunk_size", Label: "Min Chunk Size", Description: "Minimum download chunk size in MB (e.g., 2).", Type: "int64"},
			{Key: "worker_buffer_size", Label: "Worker Buffer Size", Description: "I/O buffer size per worker in KB (e.g., 512).", Type: "int"},
		},
		"Performance": {
			{Key: "max_task_retries", Label: "Max Task Retries", Description: "Number of times to retry a failed chunk before giving up.", Type: "int"},
			{Key: "slow_worker_threshold", Label: "Slow Worker Threshold", Description: "Restart workers slower than this fraction of mean speed (0.0-1.0).", Type: "float64"},
			{Key: "slow_worker_grace_period", Label: "Slow Worker Grace", Description: "Grace period before checking worker speed (e.g., 5s).", Type: "duration"},
			{Key: "stall_timeout", Label: "Stall Timeout", Description: "Restart workers with no data for this duration (e.g., 5s).", Type: "duration"},
			{Key: "speed_ema_alpha", Label: "Speed EMA Alpha", Description: "Exponential moving average smoothing factor (0.0-1.0).", Type: "float64"},
		},
	}
}

// CategoryOrder returns the order of categories for UI tabs.
func CategoryOrder() []string {
	return []string{"General", "Network", "Performance"}
}

const (
	KB = 1024
	MB = 1024 * KB
)

// DefaultSettings returns a new Settings instance with sensible defaults.
func DefaultSettings() *Settings {
	homeDir, _ := os.UserHomeDir()

	defaultDir := ""

	// Check XDG_DOWNLOAD_DIR
	if xdgDir := os.Getenv("XDG_DOWNLOAD_DIR"); xdgDir != "" {
		if info, err := os.Stat(xdgDir); err == nil && info.IsDir() {
			defaultDir = xdgDir
		}
	}

	// Check ~/Downloads if not set
	if defaultDir == "" && homeDir != "" {
		downloadsDir := filepath.Join(homeDir, "Downloads")
		if info, err := os.Stat(downloadsDir); err == nil && info.IsDir() {
			defaultDir = downloadsDir
		}
	}

	return &Settings{
		General: GeneralSettings{
			DefaultDownloadDir: defaultDir,
			WarnOnDuplicate:    true,
			ExtensionPrompt:    false,
			AutoResume:         false,

			ClipboardMonitor:  true,
			Theme:             ThemeAdaptive,
			LogRetentionCount: 5,
		},
		Network: NetworkSettings{
			MaxConnectionsPerHost:  32,
			MaxConcurrentDownloads: 3,
			UserAgent:              "", // Empty means use default UA
			SequentialDownload:     false,
			MinChunkSize:           2 * MB,
			WorkerBufferSize:       512 * KB,
		},
		Performance: PerformanceSettings{
			MaxTaskRetries:        3,
			SlowWorkerThreshold:   0.3,
			SlowWorkerGracePeriod: 5 * time.Second,
			StallTimeout:          3 * time.Second,
			SpeedEmaAlpha:         0.3,
		},
	}
}

// GetSettingsPath returns the path to the settings JSON file.
func GetSettingsPath() string {
	return filepath.Join(GetSurgeDir(), "settings.json")
}

// LoadSettings loads settings from disk. Returns defaults if file doesn't exist.
func LoadSettings() (*Settings, error) {
	path := GetSettingsPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return defaults
			return DefaultSettings(), nil
		}
		return nil, err
	}

	settings := DefaultSettings() // Start with defaults to fill any missing fields
	if err := json.Unmarshal(data, settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// SaveSettings saves settings to disk atomically.
func SaveSettings(s *Settings) error {
	path := GetSettingsPath()

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: write to temp file, then rename
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, path)
}

// ToRuntimeConfig converts Settings to a downloader RuntimeConfig
// This is used to pass user settings to the download engine
type RuntimeConfig struct {
	MaxConnectionsPerHost int
	UserAgent             string
	ProxyURL              string
	SequentialDownload    bool
	MinChunkSize          int64
	WorkerBufferSize      int
	MaxTaskRetries        int
	SlowWorkerThreshold   float64
	SlowWorkerGracePeriod time.Duration
	StallTimeout          time.Duration
	SpeedEmaAlpha         float64
}

// ToRuntimeConfig creates a RuntimeConfig from user Settings
func (s *Settings) ToRuntimeConfig() *RuntimeConfig {
	return &RuntimeConfig{
		MaxConnectionsPerHost: s.Network.MaxConnectionsPerHost,
		UserAgent:             s.Network.UserAgent,
		ProxyURL:              s.Network.ProxyURL,
		SequentialDownload:    s.Network.SequentialDownload,
		MinChunkSize:          s.Network.MinChunkSize,
		WorkerBufferSize:      s.Network.WorkerBufferSize,
		MaxTaskRetries:        s.Performance.MaxTaskRetries,
		SlowWorkerThreshold:   s.Performance.SlowWorkerThreshold,
		SlowWorkerGracePeriod: s.Performance.SlowWorkerGracePeriod,
		StallTimeout:          s.Performance.StallTimeout,
		SpeedEmaAlpha:         s.Performance.SpeedEmaAlpha,
	}
}
