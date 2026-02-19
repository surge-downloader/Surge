package health

import (
	"testing"
	"time"
)

func TestBelowRelativeMeanCullsSlowPeers(t *testing.T) {
	minUptime := 8 * time.Second
	samples := []PeerSample{
		{Key: "fast", RateBps: 2 * 1024 * 1024, Uptime: 12 * time.Second},
		{Key: "mid", RateBps: 1 * 1024 * 1024, Uptime: 12 * time.Second},
		{Key: "slow", RateBps: 128 * 1024, Uptime: 12 * time.Second},
	}

	victims := BelowRelativeMean(samples, minUptime, 0.3)
	if len(victims) != 1 || victims[0] != "slow" {
		t.Fatalf("unexpected victims: %v", victims)
	}
}

func TestBelowRelativeMeanIgnoresImmaturePeers(t *testing.T) {
	minUptime := 8 * time.Second
	samples := []PeerSample{
		{Key: "fast", RateBps: 2 * 1024 * 1024, Uptime: 12 * time.Second},
		{Key: "mid", RateBps: 1 * 1024 * 1024, Uptime: 12 * time.Second},
		{Key: "youngSlow", RateBps: 1, Uptime: 1 * time.Second},
	}

	victims := BelowRelativeMean(samples, minUptime, 0.3)
	if len(victims) != 0 {
		t.Fatalf("expected no victims, got %v", victims)
	}
}

func TestBelowRelativeMeanNeedsAtLeastTwoMaturePeers(t *testing.T) {
	minUptime := 8 * time.Second
	samples := []PeerSample{
		{Key: "onlyOne", RateBps: 1024, Uptime: 12 * time.Second},
		{Key: "young", RateBps: 1, Uptime: 1 * time.Second},
	}

	victims := BelowRelativeMean(samples, minUptime, 0.3)
	if len(victims) != 0 {
		t.Fatalf("expected no victims, got %v", victims)
	}
}
