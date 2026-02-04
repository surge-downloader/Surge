package concurrent

import (
	"context"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestHealthCheck_StallDetection(t *testing.T) {
	// Setup
	runtime := &types.RuntimeConfig{
		StallTimeout:          100 * time.Millisecond,
		SlowWorkerGracePeriod: 0,
	}
	d := &ConcurrentDownloader{
		Runtime:     runtime,
		activeTasks: make(map[int]*ActiveTask),
	}

	// Create a "stalled" task
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	stalledTask := &ActiveTask{
		StartTime:    now.Add(-1 * time.Second),
		LastActivity: now.Add(-500 * time.Millisecond).UnixNano(), // Past timeout (100ms)
		Cancel:       cancel,
	}
	// Speed is 0, but check 2 doesn't matter for this test
	stalledTask.Speed = 0

	d.activeMu.Lock()
	d.activeTasks[1] = stalledTask
	d.activeMu.Unlock()

	// Run Health Check
	d.checkWorkerHealth()

	// Verify cancellation
	select {
	case <-ctx.Done():
		// Success: context was cancelled
	default:
		t.Fatal("Stalled worker was not cancelled")
	}
}

func TestHealthCheck_StragglerDetection(t *testing.T) {
	// Setup
	runtime := &types.RuntimeConfig{
		StallTimeout:          10 * time.Second, // Long timeout so stall check passes
		SlowWorkerGracePeriod: 0,
		SlowWorkerThreshold:   0.5,
	}
	d := &ConcurrentDownloader{
		Runtime:     runtime,
		activeTasks: make(map[int]*ActiveTask),
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	now := time.Now()

	// Task 1: Fast (Baseline)
	fastTask := &ActiveTask{
		StartTime:    now.Add(-5 * time.Second),
		LastActivity: now.UnixNano(),
		Cancel:       cancel1,
		Speed:        1000,
	}

	// Task 2: Slow (Straggler)
	// Speed 100 is < 50% of mean ((1000+100)/2 = 550)
	slowTask := &ActiveTask{
		StartTime:    now.Add(-5 * time.Second),
		LastActivity: now.UnixNano(),
		Cancel:       cancel2,
		Speed:        100,
	}

	d.activeMu.Lock()
	d.activeTasks[1] = fastTask
	d.activeTasks[2] = slowTask
	d.activeMu.Unlock()

	// Run Health Check
	d.checkWorkerHealth()

	// Verify cancellation
	select {
	case <-ctx1.Done():
		t.Fatal("Fast worker was incorrectly cancelled")
	default:
		// OK
	}

	select {
	case <-ctx2.Done():
		// OK
	default:
		t.Fatal("Slow worker was NOT cancelled")
	}
}

func TestHealthCheck_ZeroSpeedDetection(t *testing.T) {
	// Setup
	runtime := &types.RuntimeConfig{
		StallTimeout:          10 * time.Second,
		SlowWorkerGracePeriod: 0,
		SlowWorkerThreshold:   0.5,
	}
	d := &ConcurrentDownloader{
		Runtime:     runtime,
		activeTasks: make(map[int]*ActiveTask),
	}

	_, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	now := time.Now()

	// Task 1: Fast
	fastTask := &ActiveTask{
		StartTime:    now.Add(-5 * time.Second),
		LastActivity: now.UnixNano(),
		Cancel:       cancel1,
		Speed:        1000,
	}

	// Task 2: Zero Speed (but not stalled by time yet)
	// Should be caught by Check 2 because 0 < threshold * mean
	zeroTask := &ActiveTask{
		StartTime:    now.Add(-5 * time.Second),
		LastActivity: now.UnixNano(),
		Cancel:       cancel2,
		Speed:        0,
	}

	d.activeMu.Lock()
	d.activeTasks[1] = fastTask
	d.activeTasks[2] = zeroTask
	d.activeMu.Unlock()

	// Run Health Check
	d.checkWorkerHealth()

	// Verify cancellation
	select {
	case <-ctx2.Done():
		// Success
	default:
		t.Fatal("Zero speed worker was NOT cancelled by relative check")
	}
}
