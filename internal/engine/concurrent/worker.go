package concurrent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

// worker downloads tasks from the queue
func (d *ConcurrentDownloader) worker(ctx context.Context, id int, mirrors []string, file *os.File, queue *TaskQueue, totalSize int64, startTime time.Time, verbose bool, client *http.Client) error {
	// Get pooled buffer
	bufPtr := d.bufPool.Get().(*[]byte)
	defer d.bufPool.Put(bufPtr)
	buf := *bufPtr

	utils.Debug("Worker %d started", id)
	defer utils.Debug("Worker %d finished", id)

	// Initial mirror assignment: Round Robin based on ID
	currentMirrorIdx := id % len(mirrors)

	for {
		// Get next task
		task, ok := queue.Pop()

		if !ok {
			return nil // Queue closed, no more work
		}

		// Update active workers
		if d.State != nil {
			d.State.ActiveWorkers.Add(1)
		}

		var lastErr error
		maxRetries := d.Runtime.GetMaxTaskRetries()
		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {

				if len(mirrors) == 1 {
					time.Sleep(time.Duration(1<<attempt) * types.RetryBaseDelay) // Exponential backoff incase of failure
				}

				// FAILOVER: Switch mirror on retry
				// Report error for the previous mirror
				d.ReportMirrorError(mirrors[currentMirrorIdx])

				currentMirrorIdx = (currentMirrorIdx + 1) % len(mirrors)
				utils.Debug("Worker %d: switching to mirror %s (attempt %d)", id, mirrors[currentMirrorIdx], attempt+1)
			}

			// Use current mirror
			currentURL := mirrors[currentMirrorIdx]

			// Register active task with per-task cancellable context
			taskCtx, taskCancel := context.WithCancel(ctx)
			now := time.Now()
			activeTask := &ActiveTask{
				Task:          task,
				CurrentOffset: task.Offset,
				StopAt:        task.Offset + task.Length,
				LastActivity:  now.UnixNano(),
				StartTime:     now,
				Cancel:        taskCancel,
				WindowStart:   now, // Initialize sliding window
			}
			d.activeMu.Lock()
			d.activeTasks[id] = activeTask
			d.activeMu.Unlock()

			// Update chunk status to Downloading
			if d.State != nil {
				utils.Debug("Worker %d: Setting range %d-%d to Downloading", id, task.Offset, task.Offset+task.Length)
				d.State.UpdateChunkStatus(task.Offset, task.Length, types.ChunkDownloading)
			} else {
				utils.Debug("Worker %d: d.State is nil, cannot update chunk status", id)
			}

			taskStart := time.Now()
			lastErr = d.downloadTask(taskCtx, currentURL, file, activeTask, buf, verbose, client, totalSize)

			// CRITICAL: Capture external cancellation state BEFORE calling taskCancel()
			// If we call taskCancel() first, taskCtx.Err() will always be non-nil
			wasExternallyCancelled := taskCtx.Err() != nil

			taskCancel() // Clean up context resources
			utils.Debug("Worker %d: Task offset=%d length=%d took %v", id, task.Offset, task.Length, time.Since(taskStart))

			// Check for PARENT context cancellation (pause/shutdown)
			// This preserves active task info for pause handler to collect
			if ctx.Err() != nil {
				// DON'T delete from activeTasks - pause handler needs it
				if d.State != nil {
					d.State.ActiveWorkers.Add(-1)
				}
				return ctx.Err()
			}

			// Check if TASK context was cancelled by Health Monitor (not by us calling taskCancel)
			// but parent context is still fine
			if wasExternallyCancelled && lastErr != nil {
				// Health monitor cancelled this task - re-queue REMAINING work only

				// Force rotation to next mirror to avoid getting stuck on the slow one
				currentMirrorIdx = (currentMirrorIdx + 1) % len(mirrors)
				utils.Debug("Worker %d: Health check cancelled task, rotating from mirror %s to %s", id, mirrors[(currentMirrorIdx+len(mirrors)-1)%len(mirrors)], mirrors[currentMirrorIdx])

				if remaining := activeTask.RemainingTask(); remaining != nil {
					// Clamp to original task end (don't go past original boundary)
					originalEnd := task.Offset + task.Length
					if remaining.Offset+remaining.Length > originalEnd {
						remaining.Length = originalEnd - remaining.Offset
					}
					if remaining.Length > 0 {
						queue.Push(*remaining)
						utils.Debug("Worker %d: health-cancelled task requeued (remaining: %d bytes from offset %d)",
							id, remaining.Length, remaining.Offset)
					}
				}
				// Delete from active tasks and move to next task (don't retry from scratch)
				d.activeMu.Lock()
				delete(d.activeTasks, id)
				d.activeMu.Unlock()
				// Clear lastErr so the fallthrough logic doesn't re-queue the original task
				lastErr = nil
				break // Exit retry loop, get next task
			}

			// Only delete from activeTasks on normal completion (not cancelled)
			d.activeMu.Lock()
			delete(d.activeTasks, id)
			d.activeMu.Unlock()

			if lastErr == nil {
				// Check if we stopped early due to stealing
				stopAt := atomic.LoadInt64(&activeTask.StopAt)
				current := atomic.LoadInt64(&activeTask.CurrentOffset)
				if current < task.Offset+task.Length && current >= stopAt {
					// We were stopped early this is expected success for the partial work
					// The stolen part is already in the queue
					utils.Debug("Worker stopped early due to stealing")
				}
				break
			}

			// Resume-on-retry: update task to reflect remaining work
			// This prevents double-counting bytes on retry
			current := atomic.LoadInt64(&activeTask.CurrentOffset)
			if current > task.Offset {
				task = types.Task{Offset: current, Length: task.Offset + task.Length - current}
			}
		}

		// Update active workers
		if d.State != nil {
			d.State.ActiveWorkers.Add(-1)
		}

		if lastErr != nil {
			// Log failed task but continue with next task
			// If we modified StopAt we should probably reset it or push the remaining part?
			// TODO: Could optimize by pushing only remaining part if we track that.
			queue.Push(task)
			utils.Debug("task at offset %d failed after %d retries: %v", task.Offset, maxRetries, lastErr)
		}
	}
}

// downloadTask downloads a single byte range and writes to file at offset
func (d *ConcurrentDownloader) downloadTask(ctx context.Context, rawurl string, file *os.File, activeTask *ActiveTask, buf []byte, verbose bool, client *http.Client, totalSize int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	task := activeTask.Task

	// Apply custom headers first (from browser extension: cookies, auth, referer, etc.)
	for key, val := range d.Headers {
		// Skip Range header - we set it ourselves for parallel downloads
		if key != "Range" {
			req.Header.Set(key, val)
		}
	}

	// Set User-Agent from config only if not provided in custom headers
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", d.Runtime.GetUserAgent())
	}
	// Range header is always set for partial downloads (overrides any browser Range header)
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", task.Offset, task.Offset+task.Length-1))

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	// Handle rate limiting explicitly
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited (429)")
	}

	// Validate status code
	if resp.StatusCode == http.StatusOK {
		// Valid only if we requested the full file
		// If we wanted a partial range but got the whole file (200), that's an error because we can't handle the full stream at a non-zero offset
		if task.Offset != 0 || task.Length != totalSize {
			return fmt.Errorf("server indicated success (200) but ignored range request (expected 206)")
		}
	} else if resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Batching State
	var pendingBytes int64
	var pendingStart int64 = -1
	lastUpdate := time.Now()
	const batchSizeThreshold = 256 * 1024 // 256KB
	const batchTimeThreshold = 100 * time.Millisecond

	// Helper to flush pending updates to global state
	flushUpdates := func() {
		if pendingBytes > 0 && d.State != nil {
			// Update Chunk Map (Global Lock)
			d.State.UpdateChunkStatus(pendingStart, pendingBytes, types.ChunkCompleted)

			// Update Downloaded Counter (Atomic)
			d.State.Downloaded.Add(pendingBytes)

			pendingBytes = 0
			pendingStart = -1
			lastUpdate = time.Now()
		}
	}
	// Ensure we flush whatever we have on exit
	defer flushUpdates()

	// Read and write at offset
	offset := task.Offset
	for {
		// Check if we should stop
		stopAt := atomic.LoadInt64(&activeTask.StopAt)
		if offset >= stopAt {
			// Stealing happened, stop here
			return nil
		}

		// Calculate how much to read to fill buffer or hit stopAt/EOF
		// We want to fill buf as much as possible to minimize WriteAt calls

		// Limit by remaining length to stopAt
		remaining := stopAt - offset
		if remaining <= 0 {
			return nil
		}

		readSize := int64(len(buf))
		if readSize > remaining {
			readSize = remaining
		}

		readSoFar := 0
		var readErr error

		for readSoFar < int(readSize) {
			n, err := resp.Body.Read(buf[readSoFar:readSize])
			if n > 0 {
				readSoFar += n
			}
			if err != nil {
				readErr = err
				break
			}
			if n == 0 {
				readErr = io.ErrUnexpectedEOF
				break
			}
		}

		if readSoFar > 0 {

			// check stopAt again before writing
			// truncate readSoFar
			currentStopAt := atomic.LoadInt64(&activeTask.StopAt)
			if offset+int64(readSoFar) > currentStopAt {
				readSoFar = int(currentStopAt - offset)
				if readSoFar <= 0 {
					return nil // stolen completely
				}
			}

			_, writeErr := file.WriteAt(buf[:readSoFar], offset)
			if writeErr != nil {
				return fmt.Errorf("write error: %w", writeErr)
			}

			now := time.Now()
			// oldOffset := offset // Unused since we use batch logic now, but logically here
			rangeStart := offset // Start of this write
			offset += int64(readSoFar)
			atomic.StoreInt64(&activeTask.CurrentOffset, offset)
			atomic.AddInt64(&activeTask.WindowBytes, int64(readSoFar))
			atomic.StoreInt64(&activeTask.LastActivity, now.UnixNano())

			// --- BATCHING LOGIC START ---

			// Calculate effective contribution (clamping to StopAt is done above via readSoFar truncation)
			// So readSoFar is exactly what we wrote and what we "own"

			if pendingStart == -1 {
				pendingStart = rangeStart
			}
			pendingBytes += int64(readSoFar)

			// Check thresholds
			if pendingBytes >= batchSizeThreshold || now.Sub(lastUpdate) >= batchTimeThreshold {
				flushUpdates()
			}

			// --- BATCHING LOGIC END ---

			// Update EMA speed using sliding window (2 second window)
			// This relies on WindowBytes which is updated atomically above, so independent of batching
			windowElapsed := now.Sub(activeTask.WindowStart).Seconds()
			if windowElapsed >= 2.0 {
				windowBytes := atomic.SwapInt64(&activeTask.WindowBytes, 0)
				recentSpeed := float64(windowBytes) / windowElapsed

				activeTask.SpeedMu.Lock()
				alpha := d.Runtime.GetSpeedEmaAlpha()
				if activeTask.Speed == 0 {
					activeTask.Speed = recentSpeed
				} else {
					activeTask.Speed = (1-alpha)*activeTask.Speed + alpha*recentSpeed
				}
				activeTask.SpeedMu.Unlock()

				activeTask.WindowStart = now // Reset window
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read error: %w", readErr)
		}
	}

	return nil
}

// StealWork tries to split an active task from a busy worker
// It greedily targets the worker with the MOST remaining work.
func (d *ConcurrentDownloader) StealWork(queue *TaskQueue) bool {
	d.activeMu.Lock()
	defer d.activeMu.Unlock()

	bestID := -1
	var maxRemaining int64 = 0
	var bestActive *ActiveTask

	// Find the worker with the MOST remaining work
	for id, active := range d.activeTasks {
		remaining := active.RemainingBytes()
		if remaining > types.MinChunk && remaining > maxRemaining {
			maxRemaining = remaining
			bestID = id
			bestActive = active
		}
	}

	if bestID == -1 {
		return false
	}

	// Found the best candidate, now try to steal
	remaining := maxRemaining
	active := bestActive

	// Split in half, aligned to AlignSize
	splitSize := alignedSplitSize(remaining)
	if splitSize == 0 {
		return false
	}

	current := atomic.LoadInt64(&active.CurrentOffset)
	newStopAt := current + splitSize

	// Update the active task stop point
	atomic.StoreInt64(&active.StopAt, newStopAt)

	finalCurrent := atomic.LoadInt64(&active.CurrentOffset)

	// The actual start of the stolen chunk must be after where the worker effectively stops.
	stolenStart := newStopAt
	if finalCurrent > newStopAt {
		stolenStart = finalCurrent
	}

	// Double check: ensure we didn't race and lose the chunk
	currentStopAt := atomic.LoadInt64(&active.StopAt)
	if stolenStart >= currentStopAt && currentStopAt != newStopAt {
		utils.Debug("StealWork race detected: stolenStart >= currentStopAt")
	}

	originalEnd := current + remaining

	if stolenStart >= originalEnd {
		return false
	}

	stolenTask := types.Task{
		Offset: stolenStart,
		Length: originalEnd - stolenStart,
	}

	queue.Push(stolenTask)
	utils.Debug("Balancer: stole %s from worker %d (new range: %d-%d)",
		utils.ConvertBytesToHumanReadable(stolenTask.Length), bestID, stolenTask.Offset, stolenTask.Offset+stolenTask.Length)

	return true
}
