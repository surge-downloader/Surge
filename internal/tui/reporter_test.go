package tui

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestProgressReporter_PollCmd_EmitsComplete(t *testing.T) {
	ps := types.NewProgressState("dl-1", 0)
	ps.StartTime = time.Now().Add(-200 * time.Millisecond)
	ps.Downloaded.Store(321)
	ps.VerifiedProgress.Store(321)
	ps.SetSavedElapsed(1 * time.Second)
	ps.Done.Store(true)

	r := NewProgressReporter(ps)
	r.pollInterval = 1 * time.Millisecond

	msg := r.PollCmd()()
	complete, ok := msg.(events.DownloadCompleteMsg)
	if !ok {
		t.Fatalf("expected DownloadCompleteMsg, got %T", msg)
	}
	if complete.DownloadID != "dl-1" {
		t.Fatalf("unexpected id: %s", complete.DownloadID)
	}
	if complete.Total != 321 {
		t.Fatalf("expected fallback total=321, got %d", complete.Total)
	}
	if complete.Elapsed < 1*time.Second {
		t.Fatalf("expected elapsed to include saved duration, got %s", complete.Elapsed)
	}
}

func TestProgressReporter_PollCmd_EmitsError(t *testing.T) {
	ps := types.NewProgressState("dl-err", 100)
	ps.SetError(errors.New("network failed"))

	r := NewProgressReporter(ps)
	r.pollInterval = 1 * time.Millisecond

	msg := r.PollCmd()()
	errMsg, ok := msg.(events.DownloadErrorMsg)
	if !ok {
		t.Fatalf("expected DownloadErrorMsg, got %T", msg)
	}
	if errMsg.DownloadID != "dl-err" || errMsg.Err == nil || errMsg.Err.Error() != "network failed" {
		t.Fatalf("unexpected error message: %+v", errMsg)
	}
}

func TestProgressReporter_PollCmd_ProgressSpeedEMA(t *testing.T) {
	ps := types.NewProgressState("dl-speed", 1000)
	ps.ActiveWorkers.Store(3)

	r := NewProgressReporter(ps)
	r.pollInterval = 1 * time.Millisecond

	ps.StartTime = time.Now().Add(-2 * time.Second)
	ps.SessionStartBytes = 0
	ps.VerifiedProgress.Store(200) // ~100 B/s

	msg1 := r.PollCmd()()
	p1, ok := msg1.(events.ProgressMsg)
	if !ok {
		t.Fatalf("expected ProgressMsg, got %T", msg1)
	}
	if p1.DownloadID != "dl-speed" || p1.Downloaded != 200 || p1.Total != 1000 || p1.ActiveConnections != 3 {
		t.Fatalf("unexpected progress payload: %+v", p1)
	}
	if p1.Speed <= 0 {
		t.Fatalf("expected positive speed, got %f", p1.Speed)
	}

	ps.StartTime = time.Now().Add(-2 * time.Second)
	ps.VerifiedProgress.Store(400) // ~200 B/s

	msg2 := r.PollCmd()()
	p2, ok := msg2.(events.ProgressMsg)
	if !ok {
		t.Fatalf("expected ProgressMsg, got %T", msg2)
	}

	want := SpeedSmoothingAlpha*200 + (1-SpeedSmoothingAlpha)*100
	if math.Abs(p2.Speed-want) > 20 { // allow timing jitter
		t.Fatalf("unexpected EMA speed: got=%f want~=%f", p2.Speed, want)
	}
}
