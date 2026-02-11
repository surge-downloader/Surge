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
	gracePeriod := d.Runtime.GetSlowWorkerGracePeriod()
	stallTimeout := d.Runtime.GetStallTimeout()
	activeCount := len(d.activeTasks)

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
		// If we have very few workers (e.g. 1), meanSpeed is just that worker's speed,
		// so "workerSpeed < mean * threshold" will never trigger.
		// Fallback to GLOBAL session speed in this case.
		if speedCount < 2 && d.State != nil {
			downloaded, _, _, sessionElapsed, _, sessionStartBytes := d.State.GetProgress()
			elapsedSeconds := sessionElapsed.Seconds()
			if elapsedSeconds > 5.0 { // Ensure we have some history
				globalSpeed := float64(downloaded-sessionStartBytes) / elapsedSeconds
				if globalSpeed > 0 {
					meanSpeed = globalSpeed
				} else {
					// Edge case: no global progress yet? use local
					meanSpeed = totalSpeed / float64(speedCount)
				}
			} else {
				meanSpeed = totalSpeed / float64(speedCount)
			}
		} else {
			meanSpeed = totalSpeed / float64(speedCount)
		}
	}

	// Second pass: check for slow workers
	for workerID, active := range d.activeTasks {

		taskDuration := now.Sub(active.StartTime)

		// Skip workers that are still in their grace period
		if taskDuration < gracePeriod {
			continue
		}

		// Hard stall guard: if no bytes have arrived for too long, cancel
		// even when measured speed is still zero/uninitialized.
		lastActivityNs := atomic.LoadInt64(&active.LastActivity)
		if lastActivityNs > 0 {
			sinceActivity := now.Sub(time.Unix(0, lastActivityNs))
			// Be less aggressive to avoid retry churn:
			// 1) New attempts with no progress yet get extra time.
			// 2) Single remaining worker gets extra time (tail phase).
			effectiveTimeout := stallTimeout
			if atomic.LoadInt64(&active.CurrentOffset) <= active.Task.Offset {
				effectiveTimeout *= 3
			}
			if activeCount == 1 {
				effectiveTimeout *= 2
			}

			if sinceActivity >= effectiveTimeout {
				utils.Debug("Health: Worker %d stalled for %v (timeout=%v), cancelling", workerID, sinceActivity, effectiveTimeout)
				if active.Cancel != nil {
					active.Cancel()
				}
				continue
			}
		}

		// Check for slow worker
		// Only cancel if: below threshold
		if meanSpeed > 0 {
			workerSpeed := active.GetSpeed()
			threshold := d.Runtime.GetSlowWorkerThreshold()
			isBelowThreshold := workerSpeed > 0 && workerSpeed < threshold*meanSpeed

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
