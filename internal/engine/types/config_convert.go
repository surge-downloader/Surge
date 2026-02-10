package types

import "github.com/surge-downloader/surge/internal/config"

// ConvertRuntimeConfig converts the app-level RuntimeConfig to the engine-level RuntimeConfig.
func ConvertRuntimeConfig(rc *config.RuntimeConfig) *RuntimeConfig {
	return &RuntimeConfig{
		MaxConnectionsPerHost: rc.MaxConnectionsPerHost,
		MaxGlobalConnections:  rc.MaxGlobalConnections,
		UserAgent:             rc.UserAgent,
		SequentialDownload:    rc.SequentialDownload,
		MinChunkSize:          rc.MinChunkSize,
		WorkerBufferSize:      rc.WorkerBufferSize,
		MaxTaskRetries:        rc.MaxTaskRetries,
		SlowWorkerThreshold:   rc.SlowWorkerThreshold,
		SlowWorkerGracePeriod: rc.SlowWorkerGracePeriod,
		StallTimeout:          rc.StallTimeout,
		SpeedEmaAlpha:         rc.SpeedEmaAlpha,
	}
}
