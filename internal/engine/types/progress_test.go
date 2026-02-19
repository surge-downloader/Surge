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
	ps.VerifiedProgress.Store(40)

	ps.SetTotalSize(200)

	if ps.TotalSize != 200 {
		t.Errorf("TotalSize = %d, want 200", ps.TotalSize)
	}
	if ps.SessionStartBytes != 40 {
		t.Errorf("SessionStartBytes = %d, want 40", ps.SessionStartBytes)
	}
}

func TestProgressState_SyncSessionStart(t *testing.T) {
	ps := NewProgressState("test", 100)
	ps.Downloaded.Store(75)
	ps.VerifiedProgress.Store(60)

	beforeSync := time.Now()
	ps.SyncSessionStart()
	afterSync := time.Now()

	if ps.SessionStartBytes != 60 {
		t.Errorf("SessionStartBytes = %d, want 60", ps.SessionStartBytes)
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
	ps.SetCancelFunc(cancel)

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
	if totalElapsed < 0 {
		t.Error("totalElapsed should not be negative")
	}
	if sessionElapsed < 0 {
		t.Error("sessionElapsed should not be negative")
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

func TestProgressState_GetProgress_PausedFreezesElapsed(t *testing.T) {
	ps := NewProgressState("test-paused-elapsed", 100)
	ps.VerifiedProgress.Store(50)
	ps.SetSavedElapsed(5 * time.Second)
	ps.StartTime = time.Now().Add(-3 * time.Second)
	ps.Pause()

	_, _, totalElapsed, sessionElapsed, _, _ := ps.GetProgress()

	if sessionElapsed != 0 {
		t.Errorf("SessionElapsed = %v, want 0 while paused", sessionElapsed)
	}
	if totalElapsed < 5*time.Second || totalElapsed > 6*time.Second {
		t.Errorf("TotalElapsed = %v, want ~5s while paused", totalElapsed)
	}
}

func TestProgressState_FinalizeSession_AccumulatesElapsed(t *testing.T) {
	ps := NewProgressState("finalize-session", 100)
	ps.VerifiedProgress.Store(80)
	ps.StartTime = time.Now().Add(-2 * time.Second)

	sessionElapsed, totalElapsed := ps.FinalizeSession(80)

	if sessionElapsed < 1500*time.Millisecond || sessionElapsed > 3*time.Second {
		t.Fatalf("sessionElapsed = %v, want around 2s", sessionElapsed)
	}
	if totalElapsed < 1500*time.Millisecond || totalElapsed > 3*time.Second {
		t.Fatalf("totalElapsed = %v, want around 2s", totalElapsed)
	}
	if got := ps.GetSavedElapsed(); got < 1500*time.Millisecond || got > 3*time.Second {
		t.Fatalf("GetSavedElapsed = %v, want around 2s", got)
	}
	if ps.SessionStartBytes != 80 {
		t.Fatalf("SessionStartBytes = %d, want 80", ps.SessionStartBytes)
	}
	if ps.VerifiedProgress.Load() != 80 {
		t.Fatalf("VerifiedProgress = %d, want 80", ps.VerifiedProgress.Load())
	}
}

func TestProgressState_FinalizePauseSession_UsesVerifiedWhenDownloadedUnknown(t *testing.T) {
	ps := NewProgressState("finalize-pause", 100)
	ps.VerifiedProgress.Store(55)
	ps.StartTime = time.Now().Add(-1200 * time.Millisecond)
	ps.Pause()

	totalElapsed := ps.FinalizePauseSession(-1)

	if totalElapsed < time.Second || totalElapsed > 2500*time.Millisecond {
		t.Fatalf("totalElapsed = %v, want around 1.2s", totalElapsed)
	}
	if ps.SessionStartBytes != 55 {
		t.Fatalf("SessionStartBytes = %d, want 55", ps.SessionStartBytes)
	}
	if ps.VerifiedProgress.Load() != 55 {
		t.Fatalf("VerifiedProgress = %d, want 55", ps.VerifiedProgress.Load())
	}
}
