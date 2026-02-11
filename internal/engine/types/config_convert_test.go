package types

import (
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/config"
)

// TestConvertRuntimeConfig_AllFieldsCopied verifies that every field in
// config.RuntimeConfig is correctly mapped to types.RuntimeConfig.
// This test would have caught the ProxyURL bug.
func TestConvertRuntimeConfig_AllFieldsCopied(t *testing.T) {
	input := &config.RuntimeConfig{
		MaxConnectionsPerHost: 48,
		UserAgent:             "TestAgent/1.0",
		ProxyURL:              "http://127.0.0.1:8080",
		SequentialDownload:    true,
		MinChunkSize:          4 * 1024 * 1024,
		WorkerBufferSize:      512 * 1024,
		MaxTaskRetries:        5,
		SlowWorkerThreshold:   0.25,
		SlowWorkerGracePeriod: 10 * time.Second,
		StallTimeout:          7 * time.Second,
		SpeedEmaAlpha:         0.4,
	}

	result := ConvertRuntimeConfig(input)

	if result == nil {
		t.Fatal("ConvertRuntimeConfig returned nil")
	}

	if result.MaxConnectionsPerHost != input.MaxConnectionsPerHost {
		t.Errorf("MaxConnectionsPerHost: got %d, want %d", result.MaxConnectionsPerHost, input.MaxConnectionsPerHost)
	}
	if result.UserAgent != input.UserAgent {
		t.Errorf("UserAgent: got %q, want %q", result.UserAgent, input.UserAgent)
	}
	if result.ProxyURL != input.ProxyURL {
		t.Errorf("ProxyURL: got %q, want %q", result.ProxyURL, input.ProxyURL)
	}
	if result.SequentialDownload != input.SequentialDownload {
		t.Errorf("SequentialDownload: got %v, want %v", result.SequentialDownload, input.SequentialDownload)
	}
	if result.MinChunkSize != input.MinChunkSize {
		t.Errorf("MinChunkSize: got %d, want %d", result.MinChunkSize, input.MinChunkSize)
	}
	if result.WorkerBufferSize != input.WorkerBufferSize {
		t.Errorf("WorkerBufferSize: got %d, want %d", result.WorkerBufferSize, input.WorkerBufferSize)
	}
	if result.MaxTaskRetries != input.MaxTaskRetries {
		t.Errorf("MaxTaskRetries: got %d, want %d", result.MaxTaskRetries, input.MaxTaskRetries)
	}
	if result.SlowWorkerThreshold != input.SlowWorkerThreshold {
		t.Errorf("SlowWorkerThreshold: got %f, want %f", result.SlowWorkerThreshold, input.SlowWorkerThreshold)
	}
	if result.SlowWorkerGracePeriod != input.SlowWorkerGracePeriod {
		t.Errorf("SlowWorkerGracePeriod: got %v, want %v", result.SlowWorkerGracePeriod, input.SlowWorkerGracePeriod)
	}
	if result.StallTimeout != input.StallTimeout {
		t.Errorf("StallTimeout: got %v, want %v", result.StallTimeout, input.StallTimeout)
	}
	if result.SpeedEmaAlpha != input.SpeedEmaAlpha {
		t.Errorf("SpeedEmaAlpha: got %f, want %f", result.SpeedEmaAlpha, input.SpeedEmaAlpha)
	}
}

// TestConvertRuntimeConfig_EmptyProxyURL ensures empty proxy doesn't cause issues.
func TestConvertRuntimeConfig_EmptyProxyURL(t *testing.T) {
	input := &config.RuntimeConfig{
		MaxConnectionsPerHost: 32,
		ProxyURL:              "",
	}

	result := ConvertRuntimeConfig(input)

	if result.ProxyURL != "" {
		t.Errorf("ProxyURL: got %q, want empty", result.ProxyURL)
	}
}
