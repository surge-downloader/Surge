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
		MaxConnectionsPerHost:  48,
		UserAgent:              "TestAgent/1.0",
		ProxyURL:               "http://127.0.0.1:8080",
		SequentialDownload:     true,
		MinChunkSize:           4 * 1024 * 1024,
		WorkerBufferSize:       512 * 1024,
		MaxTaskRetries:         5,
		SlowWorkerThreshold:    0.25,
		SlowWorkerGracePeriod:  10 * time.Second,
		StallTimeout:           7 * time.Second,
		SpeedEmaAlpha:          0.4,
		TorrentMaxConnections:  80,
		TorrentUploadSlots:     6,
		TorrentRequestPipeline: 12,
		TorrentListenPort:      6881,
		TorrentHealthEnabled:   true,
		TorrentLowRateCull:     0.3,
		TorrentHealthMinUptime: 30 * time.Second,
		TorrentHealthCullMax:   3,
		TorrentHealthRedial:    90 * time.Second,
		TorrentEvictionCD:      7 * time.Second,
		TorrentEvictionMinUp:   22 * time.Second,
		TorrentEvictionIdle:    40 * time.Second,
		TorrentEvictionMinBps:  256 * 1024,
		TorrentPeerReadTO:      55 * time.Second,
		TorrentPeerKeepAlive:   25 * time.Second,
		TorrentTrackerNormal:   6 * time.Second,
		TorrentTrackerLowPeer:  3 * time.Second,
		TorrentTrackerWant:     240,
		TorrentTrackerWantLow:  320,
		TorrentLSDEnabled:      true,
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
	if result.TorrentMaxConnections != input.TorrentMaxConnections {
		t.Errorf("TorrentMaxConnections: got %d, want %d", result.TorrentMaxConnections, input.TorrentMaxConnections)
	}
	if result.TorrentUploadSlots != input.TorrentUploadSlots {
		t.Errorf("TorrentUploadSlots: got %d, want %d", result.TorrentUploadSlots, input.TorrentUploadSlots)
	}
	if result.TorrentRequestPipeline != input.TorrentRequestPipeline {
		t.Errorf("TorrentRequestPipeline: got %d, want %d", result.TorrentRequestPipeline, input.TorrentRequestPipeline)
	}
	if result.TorrentListenPort != input.TorrentListenPort {
		t.Errorf("TorrentListenPort: got %d, want %d", result.TorrentListenPort, input.TorrentListenPort)
	}
	if result.TorrentHealthEnabled != input.TorrentHealthEnabled {
		t.Errorf("TorrentHealthEnabled: got %v, want %v", result.TorrentHealthEnabled, input.TorrentHealthEnabled)
	}
	if result.TorrentLowRateCull != input.TorrentLowRateCull {
		t.Errorf("TorrentLowRateCull: got %f, want %f", result.TorrentLowRateCull, input.TorrentLowRateCull)
	}
	if result.TorrentHealthMinUptime != input.TorrentHealthMinUptime {
		t.Errorf("TorrentHealthMinUptime: got %v, want %v", result.TorrentHealthMinUptime, input.TorrentHealthMinUptime)
	}
	if result.TorrentHealthCullMax != input.TorrentHealthCullMax {
		t.Errorf("TorrentHealthCullMax: got %d, want %d", result.TorrentHealthCullMax, input.TorrentHealthCullMax)
	}
	if result.TorrentHealthRedial != input.TorrentHealthRedial {
		t.Errorf("TorrentHealthRedial: got %v, want %v", result.TorrentHealthRedial, input.TorrentHealthRedial)
	}
	if result.TorrentEvictionCD != input.TorrentEvictionCD {
		t.Errorf("TorrentEvictionCD: got %v, want %v", result.TorrentEvictionCD, input.TorrentEvictionCD)
	}
	if result.TorrentEvictionMinUp != input.TorrentEvictionMinUp {
		t.Errorf("TorrentEvictionMinUp: got %v, want %v", result.TorrentEvictionMinUp, input.TorrentEvictionMinUp)
	}
	if result.TorrentEvictionIdle != input.TorrentEvictionIdle {
		t.Errorf("TorrentEvictionIdle: got %v, want %v", result.TorrentEvictionIdle, input.TorrentEvictionIdle)
	}
	if result.TorrentEvictionMinBps != input.TorrentEvictionMinBps {
		t.Errorf("TorrentEvictionMinBps: got %d, want %d", result.TorrentEvictionMinBps, input.TorrentEvictionMinBps)
	}
	if result.TorrentPeerReadTO != input.TorrentPeerReadTO {
		t.Errorf("TorrentPeerReadTO: got %v, want %v", result.TorrentPeerReadTO, input.TorrentPeerReadTO)
	}
	if result.TorrentPeerKeepAlive != input.TorrentPeerKeepAlive {
		t.Errorf("TorrentPeerKeepAlive: got %v, want %v", result.TorrentPeerKeepAlive, input.TorrentPeerKeepAlive)
	}
	if result.TorrentTrackerNormal != input.TorrentTrackerNormal {
		t.Errorf("TorrentTrackerNormal: got %v, want %v", result.TorrentTrackerNormal, input.TorrentTrackerNormal)
	}
	if result.TorrentTrackerLowPeer != input.TorrentTrackerLowPeer {
		t.Errorf("TorrentTrackerLowPeer: got %v, want %v", result.TorrentTrackerLowPeer, input.TorrentTrackerLowPeer)
	}
	if result.TorrentTrackerWant != input.TorrentTrackerWant {
		t.Errorf("TorrentTrackerWant: got %d, want %d", result.TorrentTrackerWant, input.TorrentTrackerWant)
	}
	if result.TorrentTrackerWantLow != input.TorrentTrackerWantLow {
		t.Errorf("TorrentTrackerWantLow: got %d, want %d", result.TorrentTrackerWantLow, input.TorrentTrackerWantLow)
	}
	if result.TorrentLSDEnabled != input.TorrentLSDEnabled {
		t.Errorf("TorrentLSDEnabled: got %v, want %v", result.TorrentLSDEnabled, input.TorrentLSDEnabled)
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
