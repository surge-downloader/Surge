package concurrent

import (
	"context"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestHealth_LastManStanding(t *testing.T) {
	// 1. Setup mock state with high historical speed
	// Say we downloaded 100MB in 10s => 10MB/s global average
	state := types.NewProgressState("test", 1000)
	state.VerifiedProgress.Store(100 * 1024 * 1024)

	runtime := &types.RuntimeConfig{
		SlowWorkerThreshold:   0.5,
		SlowWorkerGracePeriod: 0, // Instant check
	}

	d := NewConcurrentDownloader("test", nil, state, runtime)

	// 2. Add one active task that is SLOW
	// Global is 10MB/s (100MB / 10s)
	// Worker is 1MB/s (should be < 0.5 * 10 = 5MB/s).

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()

	// Hack: Set State.StartTime to 10s ago
	// This is safe here because we are single-threaded in setup
	state.StartTime = now.Add(-10 * time.Second)

	active := &ActiveTask{
		StartTime: now.Add(-10 * time.Second), // Started long ago
		Speed:     1 * 1024 * 1024,            // 1 MB/s
		Cancel:    cancel,
	}

	d.activeTasks[0] = active

	// 3. Run Check
	d.checkWorkerHealth()

	// 4. Verify Cancellation
	select {
	case <-ctx.Done():
		// Success: context cancelled
	default:
		t.Errorf("Worker should have been cancelled (Global Speed ~10MB/s, Worker 1MB/s)")
	}
}

func TestHealth_MultipleWorkers(t *testing.T) {
	runtime := &types.RuntimeConfig{
		SlowWorkerThreshold:   0.5,
		SlowWorkerGracePeriod: 0,
	}
	state := types.NewProgressState("test", 1000)
	d := NewConcurrentDownloader("test", nil, state, runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()

	// 1. Setup multiple workers
	// Worker 0: 10 MB/s
	// Worker 1: 10 MB/s
	// Worker 2: 1 MB/s (Slow)
	// Mean = 7 MB/s. Threshold = 3.5 MB/s. Worker 2 < 3.5 => Cancel.

	w0Ctx, w0Cancel := context.WithCancel(ctx)
	w1Ctx, w1Cancel := context.WithCancel(ctx)
	w2Ctx, w2Cancel := context.WithCancel(ctx)

	d.activeTasks[0] = &ActiveTask{StartTime: now.Add(-10 * time.Second), Speed: 10 * 1024 * 1024, Cancel: w0Cancel}
	d.activeTasks[1] = &ActiveTask{StartTime: now.Add(-10 * time.Second), Speed: 10 * 1024 * 1024, Cancel: w1Cancel}
	d.activeTasks[2] = &ActiveTask{StartTime: now.Add(-10 * time.Second), Speed: 1 * 1024 * 1024, Cancel: w2Cancel}

	d.checkWorkerHealth()

	// Verify Worker 2 cancelled
	select {
	case <-w2Ctx.Done():
		// Success
	default:
		t.Error("Worker 2 should have been cancelled")
	}

	// Verify others NOT cancelled
	select {
	case <-w0Ctx.Done():
		t.Error("Worker 0 should NOT have been cancelled")
	default:
	}
	select {
	case <-w1Ctx.Done():
		t.Error("Worker 1 should NOT have been cancelled")
	default:
	}
}

func TestHealth_GracePeriod(t *testing.T) {
	runtime := &types.RuntimeConfig{
		SlowWorkerThreshold:   0.5,
		SlowWorkerGracePeriod: 5 * time.Second,
	}
	state := types.NewProgressState("test", 1000)
	d := NewConcurrentDownloader("test", nil, state, runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()

	// 1. Setup workers
	// Worker 0: 10 MB/s (Old)
	// Worker 1: 0.1 MB/s (New, within grace period) -> Should NOT cancel despite being slow

	w0Ctx, w0Cancel := context.WithCancel(ctx)
	w1Ctx, w1Cancel := context.WithCancel(ctx)

	d.activeTasks[0] = &ActiveTask{StartTime: now.Add(-10 * time.Second), Speed: 10 * 1024 * 1024, Cancel: w0Cancel}
	d.activeTasks[1] = &ActiveTask{StartTime: now.Add(-1 * time.Second), Speed: 100 * 1024, Cancel: w1Cancel}

	d.checkWorkerHealth()

	// Verify Worker 1 NOT cancelled due to grace period
	select {
	case <-w1Ctx.Done():
		t.Error("Worker 1 should NOT have been cancelled (Grace Period)")
	default:
		// Success
	}

	// Verify Worker 0 NOT cancelled (Fast enough)
	select {
	case <-w0Ctx.Done():
		t.Error("Worker 0 should NOT have been cancelled")
	default:
	}
}

func TestHealth_StalledWorkerCancelledEvenWithZeroSpeed(t *testing.T) {
	runtime := &types.RuntimeConfig{
		SlowWorkerThreshold:   0.5,
		SlowWorkerGracePeriod: 0,
		StallTimeout:          2 * time.Second,
	}
	state := types.NewProgressState("test", 1000)
	d := NewConcurrentDownloader("test", nil, state, runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	d.activeTasks[0] = &ActiveTask{
		Task:          types.Task{Offset: 0, Length: 1024},
		StartTime:     now.Add(-10 * time.Second),
		CurrentOffset: 512, // Simulate that this attempt had made progress before stalling
		LastActivity:  now.Add(-5 * time.Second).UnixNano(),
		Speed:         0, // Zero speed should still be treated as stall
		Cancel:        cancel,
	}

	d.checkWorkerHealth()

	select {
	case <-ctx.Done():
		// Success
	default:
		t.Error("Worker should have been cancelled due to stall timeout")
	}
}

func TestHealth_RecentActivityNotCancelledByStallGuard(t *testing.T) {
	runtime := &types.RuntimeConfig{
		SlowWorkerThreshold:   0.5,
		SlowWorkerGracePeriod: 0,
		StallTimeout:          5 * time.Second,
	}
	state := types.NewProgressState("test", 1000)
	d := NewConcurrentDownloader("test", nil, state, runtime)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	now := time.Now()
	d.activeTasks[0] = &ActiveTask{
		StartTime:    now.Add(-10 * time.Second),
		LastActivity: now.Add(-500 * time.Millisecond).UnixNano(),
		Speed:        0,
		Cancel:       cancel,
	}

	d.checkWorkerHealth()

	select {
	case <-ctx.Done():
		t.Error("Worker should NOT have been cancelled with recent activity")
	default:
		// Success
	}
}
