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

	// Second pass: check for slow and stalled workers
	stallTimeout := d.Runtime.GetStallTimeout()
	for workerID, active := range d.activeTasks {

		// timeSinceActivity := now.Sub(lastTime)
		taskDuration := now.Sub(active.StartTime)

		// Skip workers that are still in their grace period
		gracePeriod := d.Runtime.GetSlowWorkerGracePeriod()
		if taskDuration < gracePeriod {
			continue
		}

		// Check for absolute stall: no data received for StallTimeout
		// This catches dead connections that the relative speed check misses
		lastActivity := atomic.LoadInt64(&active.LastActivity)
		if lastActivity > 0 {
			timeSinceData := now.Sub(time.Unix(0, lastActivity))
			if timeSinceData >= stallTimeout {
				utils.Debug("Health: Worker %d stalled (no data for %v), cancelling",
					workerID, timeSinceData.Truncate(time.Millisecond))
				if active.Cancel != nil {
					active.Cancel()
				}
				continue // Already cancelled, skip speed check
			}
		}

		// Check for slow worker (relative speed)
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
