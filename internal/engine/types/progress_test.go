package types

import (
	"context"
	"testing"
	"time"
)

func TestNewProgressState(t *testing.T) {
	ps := NewProgressState("test-id", 1000)

	if ps.ID != "test-id" {
		t.Errorf("ID = %s, want test-id", ps.ID)
	}
	if ps.TotalSize != 1000 {
		t.Errorf("TotalSize = %d, want 1000", ps.TotalSize)
	}
	if ps.Downloaded.Load() != 0 {
		t.Errorf("Downloaded = %d, want 0", ps.Downloaded.Load())
	}
	if ps.ActiveWorkers.Load() != 0 {
		t.Errorf("ActiveWorkers = %d, want 0", ps.ActiveWorkers.Load())
	}
	if ps.Done.Load() {
		t.Error("Done should be false initially")
	}
	if ps.Paused.Load() {
		t.Error("Paused should be false initially")
	}
}

func TestProgressState_SetTotalSize(t *testing.T) {
	ps := NewProgressState("test", 100)
	ps.Downloaded.Store(50)

	ps.SetTotalSize(200)

	if ps.TotalSize != 200 {
		t.Errorf("TotalSize = %d, want 200", ps.TotalSize)
	}
	if ps.SessionStartBytes != 50 {
		t.Errorf("SessionStartBytes = %d, want 50", ps.SessionStartBytes)
	}
}

func TestProgressState_SyncSessionStart(t *testing.T) {
	ps := NewProgressState("test", 100)
	ps.Downloaded.Store(75)

	beforeSync := time.Now()
	ps.SyncSessionStart()
	afterSync := time.Now()

	if ps.SessionStartBytes != 75 {
		t.Errorf("SessionStartBytes = %d, want 75", ps.SessionStartBytes)
	}
	if ps.StartTime.Before(beforeSync) || ps.StartTime.After(afterSync) {
		t.Error("StartTime should be updated to current time")
	}
}

func TestProgressState_Error(t *testing.T) {
	ps := NewProgressState("test", 100)

	// Initially no error
	if err := ps.GetError(); err != nil {
		t.Errorf("GetError = %v, want nil", err)
	}

	// Set error
	testErr := context.DeadlineExceeded
	ps.SetError(testErr)

	if err := ps.GetError(); err != testErr {
		t.Errorf("GetError = %v, want %v", err, testErr)
	}
}

func TestProgressState_PauseResume(t *testing.T) {
	ps := NewProgressState("test", 100)

	// Initially not paused
	if ps.IsPaused() {
		t.Error("Should not be paused initially")
	}

	// Pause
	ps.Pause()
	if !ps.IsPaused() {
		t.Error("Should be paused after Pause()")
	}

	// Resume
	ps.Resume()
	if ps.IsPaused() {
		t.Error("Should not be paused after Resume()")
	}
}

func TestProgressState_PauseWithCancelFunc(t *testing.T) {
	ps := NewProgressState("test", 100)

	ctx, cancel := context.WithCancel(context.Background())
	ps.CancelFunc = cancel

	// Verify context is not cancelled
	select {
	case <-ctx.Done():
		t.Fatal("Context should not be cancelled yet")
	default:
	}

	// Pause should also cancel context
	ps.Pause()

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("Context should be cancelled after Pause()")
	}
}

func TestProgressState_GetProgress(t *testing.T) {
	ps := NewProgressState("test", 1000)
	ps.VerifiedProgress.Store(500)
	ps.ActiveWorkers.Store(4)
	ps.SessionStartBytes = 100

	downloaded, total, totalElapsed, sessionElapsed, connections, sessionStart := ps.GetProgress()

	if downloaded != 500 {
		t.Errorf("downloaded = %d, want 500", downloaded)
	}
	if total != 1000 {
		t.Errorf("total = %d, want 1000", total)
	}
	if totalElapsed <= 0 {
		t.Error("totalElapsed should be positive")
	}
	if sessionElapsed <= 0 {
		t.Error("sessionElapsed should be positive")
	}
	if connections != 4 {
		t.Errorf("connections = %d, want 4", connections)
	}
	if sessionStart != 100 {
		t.Errorf("sessionStart = %d, want 100", sessionStart)
	}
}

func TestProgressState_AtomicOperations(t *testing.T) {
	ps := NewProgressState("test", 1000)

	// Test concurrent increment
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			ps.Downloaded.Add(100)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if ps.Downloaded.Load() != 1000 {
		t.Errorf("Downloaded = %d, want 1000 after 10 concurrent adds of 100", ps.Downloaded.Load())
	}
}
func TestProgressState_ElapsedCalculation(t *testing.T) {
	ps := NewProgressState("test-elapsed", 100)

	// Simulate previous session
	savedElapsed := 5 * time.Second
	ps.SetSavedElapsed(savedElapsed)

	// Simulate current session start 2 seconds ago
	ps.StartTime = time.Now().Add(-2 * time.Second)

	_, _, totalElapsed, sessionElapsed, _, _ := ps.GetProgress()

	// Verify Session Elapsed is approx 2s
	if sessionElapsed < 1*time.Second || sessionElapsed > 3*time.Second {
		t.Errorf("SessionElapsed = %v, want ~2s", sessionElapsed)
	}

	// Verify Total Elapsed is approx 7s (5s + 2s)
	if totalElapsed < 6*time.Second || totalElapsed > 8*time.Second {
		t.Errorf("TotalElapsed = %v, want ~7s", totalElapsed)
	}
}
