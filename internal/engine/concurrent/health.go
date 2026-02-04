package concurrent

import (
	"sync/atomic"
	"time"

	"github.com/surge-downloader/surge/internal/utils"
)

// checkWorkerHealth detects slow workers and cancels them
func (d *ConcurrentDownloader) checkWorkerHealth() {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	if len(d.activeTasks) == 0 {
		return
	}

	now := time.Now()
	stallTimeout := d.Runtime.GetStallTimeout()
	gracePeriod := d.Runtime.GetSlowWorkerGracePeriod()

	// First pass: calculate mean speed
	var totalSpeed float64
	var speedCount int
	for _, active := range d.activeTasks {
		if speed := active.GetSpeed(); speed > 0 {
			totalSpeed += speed
			speedCount++
		}
	}

	var meanSpeed float64
	if speedCount > 0 {
		meanSpeed = totalSpeed / float64(speedCount)
	}

	// Second pass: check for slow workers
	for workerID, active := range d.activeTasks {

		// ---------------------------------------------------------
		// CHECK 1: The Hard Stall (The "Zombie" Killer)
		// ---------------------------------------------------------
		// If the worker hasn't written a single byte for > StallTimeout (5s),
		// it is dead. Kill it immediately. Speed is irrelevant here.
		lastActivityNano := atomic.LoadInt64(&active.LastActivity)
		lastActivity := time.Unix(0, lastActivityNano)
		timeSinceLastActivity := now.Sub(lastActivity)

		if timeSinceLastActivity > stallTimeout {
			utils.Debug("Health: Worker %d stalled. No activity for %v. Cancelling.",
				workerID, timeSinceLastActivity)

			if active.Cancel != nil {
				active.Cancel()
			}
			continue
		}

		// ---------------------------------------------------------
		// CHECK 2: The Relative Slowness (The "Straggler" Killer)
		// ---------------------------------------------------------
		taskDuration := now.Sub(active.StartTime)

		// SKIP if in grace period
		if taskDuration < gracePeriod {
			continue
		}

		// Only run this if we have a valid global baseline
		if meanSpeed > 0 {
			workerSpeed := active.GetSpeed()

			// BUG FIX: Removed "workerSpeed > 0".
			// If speed is 0 (and not caught by stall check yet), it IS below threshold.
			threshold := d.Runtime.GetSlowWorkerThreshold()
			isBelowThreshold := workerSpeed < threshold*meanSpeed

			if isBelowThreshold {
				utils.Debug("Health: Worker %d slow (%.2f KB/s vs mean %.2f KB/s), cancelling",
					workerID, workerSpeed/1024, meanSpeed/1024)
				if active.Cancel != nil {
					active.Cancel()
				}
			}
		}
	}
}
