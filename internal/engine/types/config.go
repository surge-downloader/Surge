package types

import (
	"time"
)

// Size constants
const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB

	// Megabyte as float for display calculations
	Megabyte = 1024.0 * 1024.0

	// IncompleteSuffix is appended to files while downloading
	IncompleteSuffix = ".surge"
)

// Chunk size constants for concurrent downloads
const (
	MinChunk     = 2 * MB // Minimum chunk size
	AlignSize    = 4 * KB // Align chunks to 4KB for filesystem
	WorkerBuffer = 512 * KB

	// Batching constants for worker updates
	WorkerBatchSize     = 1 * MB                 // Batch updates until 1MB is downloaded
	WorkerBatchInterval = 200 * time.Millisecond // Or until 200ms passes
)

// Connection limits
const (
	PerHostMax = 64 // Max concurrent connections per host
)

// HTTP Client Tuning
const (
	DefaultMaxIdleConns          = 100
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 15 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second
	DialTimeout                  = 10 * time.Second
	KeepAliveDuration            = 30 * time.Second
	ProbeTimeout                 = 30 * time.Second
)

// Channel buffer sizes
const (
	ProgressChannelBuffer = 100
)

// DownloadConfig contains all parameters needed to start a download
type DownloadConfig struct {
	URL        string
	OutputPath string
	DestPath   string // Full destination path (for resume state lookup)
	ID         string
	Filename   string
	IsResume   bool // True if this is explicitly a resume, not a fresh download
	ProgressCh chan<- any
	State      *ProgressState
	SavedState *DownloadState    // Pre-loaded state for resume optimization
	Runtime    *RuntimeConfig    // Dynamic settings from user config
	Mirrors    []string          // List of mirror URLs (including primary)
	Headers    map[string]string // Custom HTTP headers from browser (cookies, auth, etc.)
}

// RuntimeConfig holds dynamic settings that can override defaults
type RuntimeConfig struct {
	MaxConnectionsPerHost int
	UserAgent             string
	ProxyURL              string
	SequentialDownload    bool
	MinChunkSize          int64

	WorkerBufferSize       int
	MaxTaskRetries         int
	SlowWorkerThreshold    float64
	SlowWorkerGracePeriod  time.Duration
	StallTimeout           time.Duration
	SpeedEmaAlpha          float64
	TorrentMaxConnections  int
	TorrentUploadSlots     int
	TorrentRequestPipeline int
	TorrentListenPort      int
	TorrentHealthEnabled   bool
	TorrentLowRateCull     float64
	TorrentHealthMinUptime time.Duration
	TorrentHealthCullMax   int
	TorrentHealthRedial    time.Duration
	TorrentEvictionCD      time.Duration
	TorrentEvictionMinUp   time.Duration
	TorrentEvictionIdle    time.Duration
	TorrentEvictionMinBps  int64
	TorrentPeerReadTO      time.Duration
	TorrentPeerKeepAlive   time.Duration
	TorrentTrackerNormal   time.Duration
	TorrentTrackerLowPeer  time.Duration
	TorrentTrackerWant     int
	TorrentTrackerWantLow  int
	TorrentLSDEnabled      bool
}

// GetUserAgent returns the configured user agent or the default
func (r *RuntimeConfig) GetUserAgent() string {
	if r == nil || r.UserAgent == "" {
		return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	}
	return r.UserAgent
}

// GetMaxConnectionsPerHost returns configured value or default
func (r *RuntimeConfig) GetMaxConnectionsPerHost() int {
	if r == nil || r.MaxConnectionsPerHost <= 0 {
		return PerHostMax
	}
	return r.MaxConnectionsPerHost
}

// GetMinChunkSize returns configured value or default
func (r *RuntimeConfig) GetMinChunkSize() int64 {
	if r == nil || r.MinChunkSize <= 0 {
		return MinChunk
	}
	return r.MinChunkSize
}

// GetWorkerBufferSize returns configured value or default
func (r *RuntimeConfig) GetWorkerBufferSize() int {
	if r == nil || r.WorkerBufferSize <= 0 {
		return WorkerBuffer
	}
	return r.WorkerBufferSize
}

func (r *RuntimeConfig) GetTorrentMaxConnections() int {
	if r == nil || r.TorrentMaxConnections <= 0 {
		return 256
	}
	if r.TorrentMaxConnections > 1000 {
		return 1000
	}
	return r.TorrentMaxConnections
}

func (r *RuntimeConfig) GetTorrentUploadSlots() int {
	if r == nil || r.TorrentUploadSlots < 0 {
		return 32
	}
	if r.TorrentUploadSlots > 200 {
		return 200
	}
	return r.TorrentUploadSlots
}

func (r *RuntimeConfig) GetTorrentRequestPipeline() int {
	if r == nil || r.TorrentRequestPipeline <= 0 {
		return 64
	}
	if r.TorrentRequestPipeline > 256 {
		return 256
	}
	return r.TorrentRequestPipeline
}

func (r *RuntimeConfig) GetTorrentListenPort() int {
	if r == nil || r.TorrentListenPort <= 0 || r.TorrentListenPort > 65535 {
		return 6881
	}
	return r.TorrentListenPort
}

func (r *RuntimeConfig) GetTorrentHealthEnabled() bool {
	if r == nil {
		return true
	}
	return r.TorrentHealthEnabled
}

func (r *RuntimeConfig) GetTorrentLowRateCullFactor() float64 {
	if r == nil || r.TorrentLowRateCull <= 0 {
		return 0.3
	}
	if r.TorrentLowRateCull > 1 {
		return 1
	}
	return r.TorrentLowRateCull
}

func (r *RuntimeConfig) GetTorrentHealthMinUptime() time.Duration {
	if r == nil || r.TorrentHealthMinUptime <= 0 {
		return 20 * time.Second
	}
	return r.TorrentHealthMinUptime
}

func (r *RuntimeConfig) GetTorrentHealthCullMaxPerTick() int {
	if r == nil || r.TorrentHealthCullMax <= 0 {
		return 2
	}
	if r.TorrentHealthCullMax > 16 {
		return 16
	}
	return r.TorrentHealthCullMax
}

func (r *RuntimeConfig) GetTorrentHealthRedialBlock() time.Duration {
	if r == nil || r.TorrentHealthRedial <= 0 {
		return 2 * time.Minute
	}
	return r.TorrentHealthRedial
}

func (r *RuntimeConfig) GetTorrentEvictionCooldown() time.Duration {
	if r == nil || r.TorrentEvictionCD <= 0 {
		return 5 * time.Second
	}
	return r.TorrentEvictionCD
}

func (r *RuntimeConfig) GetTorrentEvictionMinUptime() time.Duration {
	if r == nil || r.TorrentEvictionMinUp <= 0 {
		return 20 * time.Second
	}
	return r.TorrentEvictionMinUp
}

func (r *RuntimeConfig) GetTorrentIdleEvictionThreshold() time.Duration {
	if r == nil || r.TorrentEvictionIdle <= 0 {
		return 45 * time.Second
	}
	return r.TorrentEvictionIdle
}

func (r *RuntimeConfig) GetTorrentEvictionKeepRateMinimumBps() int64 {
	if r == nil || r.TorrentEvictionMinBps <= 0 {
		return 512 * KB
	}
	return r.TorrentEvictionMinBps
}

func (r *RuntimeConfig) GetTorrentPeerReadTimeout() time.Duration {
	if r == nil || r.TorrentPeerReadTO <= 0 {
		return 45 * time.Second
	}
	return r.TorrentPeerReadTO
}

func (r *RuntimeConfig) GetTorrentPeerKeepAliveSendInterval() time.Duration {
	if r == nil || r.TorrentPeerKeepAlive <= 0 {
		return 30 * time.Second
	}
	return r.TorrentPeerKeepAlive
}

func (r *RuntimeConfig) GetTorrentTrackerIntervalNormal() time.Duration {
	if r == nil || r.TorrentTrackerNormal <= 0 {
		return 5 * time.Second
	}
	return r.TorrentTrackerNormal
}

func (r *RuntimeConfig) GetTorrentTrackerIntervalLowPeer() time.Duration {
	if r == nil || r.TorrentTrackerLowPeer <= 0 {
		return 3 * time.Second
	}
	return r.TorrentTrackerLowPeer
}

func (r *RuntimeConfig) GetTorrentTrackerNumWantNormal() int {
	if r == nil || r.TorrentTrackerWant <= 0 {
		return 256
	}
	if r.TorrentTrackerWant > 1000 {
		return 1000
	}
	return r.TorrentTrackerWant
}

func (r *RuntimeConfig) GetTorrentTrackerNumWantLowPeer() int {
	if r == nil || r.TorrentTrackerWantLow <= 0 {
		return 300
	}
	if r.TorrentTrackerWantLow > 1000 {
		return 1000
	}
	return r.TorrentTrackerWantLow
}

func (r *RuntimeConfig) GetTorrentLSDEnabled() bool {
	if r == nil {
		return true
	}
	return r.TorrentLSDEnabled
}

const (
	MaxTaskRetries = 3
	RetryBaseDelay = 200 * time.Millisecond

	// Health check constants
	HealthCheckInterval = 1 * time.Second // How often to check worker health
	SlowWorkerThreshold = 0.50            // Restart if speed < x times of mean
	SlowWorkerGrace     = 5 * time.Second // Grace period before checking speed
	StallTimeout        = 5 * time.Second // Restart if no data for x seconds
	SpeedEMAAlpha       = 0.3             // EMA smoothing factor
)

// GetMaxTaskRetries returns configured value or default
func (r *RuntimeConfig) GetMaxTaskRetries() int {
	if r == nil || r.MaxTaskRetries <= 0 {
		return MaxTaskRetries
	}
	return r.MaxTaskRetries
}

// GetSlowWorkerThreshold returns configured value or default
func (r *RuntimeConfig) GetSlowWorkerThreshold() float64 {
	if r == nil || r.SlowWorkerThreshold <= 0 {
		return SlowWorkerThreshold
	}
	return r.SlowWorkerThreshold
}

// GetSlowWorkerGracePeriod returns configured value or default
func (r *RuntimeConfig) GetSlowWorkerGracePeriod() time.Duration {
	if r == nil || r.SlowWorkerGracePeriod <= 0 {
		return SlowWorkerGrace
	}
	return r.SlowWorkerGracePeriod
}

// GetStallTimeout returns configured value or default
func (r *RuntimeConfig) GetStallTimeout() time.Duration {
	if r == nil || r.StallTimeout <= 0 {
		return StallTimeout
	}
	return r.StallTimeout
}

// GetSpeedEmaAlpha returns configured value or default
func (r *RuntimeConfig) GetSpeedEmaAlpha() float64 {
	if r == nil || r.SpeedEmaAlpha <= 0 {
		return SpeedEMAAlpha
	}
	return r.SpeedEmaAlpha
}
