package concurrent

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestStealWork(t *testing.T) {
	// Setup
	downloader := &ConcurrentDownloader{
		activeTasks: make(map[int]*ActiveTask),
	}
	queue := NewTaskQueue()

	// Case 1: No active tasks
	if downloader.StealWork(queue) {
		t.Error("StealWork should return false when no active tasks")
	}

	// Case 2: Active task with little work (less than MinChunk)
	// MinChunk is 2MB based on our check
	smallTask := &ActiveTask{
		Task: types.Task{Offset: 0, Length: types.MinChunk}, // Just enough to NOT be stolen if we split?
		// Worker logic: current=0, stopAt=MinChunk. Remaining = MinChunk.
		// Split logic: half = MinChunk/2. If half < MinChunk -> return 0.
	}
	atomic.StoreInt64(&smallTask.CurrentOffset, 0)
	atomic.StoreInt64(&smallTask.StopAt, types.MinChunk) // 2MB

	downloader.activeTasks[0] = smallTask

	if downloader.StealWork(queue) {
		t.Error("StealWork should return false when remaining work is small")
	}

	// Case 3: Active task with LOTS of work
	// 100MB
	largeSize := int64(100 * 1024 * 1024)
	largeTask := &ActiveTask{
		Task:      types.Task{Offset: 0, Length: largeSize},
		StartTime: time.Now(),
	}
	atomic.StoreInt64(&largeTask.CurrentOffset, 0)
	atomic.StoreInt64(&largeTask.StopAt, largeSize)

	downloader.activeTasks[1] = largeTask

	success := downloader.StealWork(queue)

	if !success {
		t.Error("StealWork should return true for large task")
	}

	if queue.Len() != 1 {
		t.Errorf("Queue should have 1 task, got %d", queue.Len())
	}

	// Check Task Details
	// alignedSplitSize: 100MB / 2 = 50MB. Aligned to 4KB (50MB is already aligned).
	// New StopAt for active task should be 50MB.
	expectedStopAt := largeSize / 2

	currentStopAt := atomic.LoadInt64(&largeTask.StopAt)
	if currentStopAt != expectedStopAt {
		t.Errorf("Active task StopAt should be %d, got %d", expectedStopAt, currentStopAt)
	}

	// Check Stolen Task
	stolenTask, _ := queue.Pop()
	if stolenTask.Offset != expectedStopAt {
		t.Errorf("Stolen task offset should be %d, got %d", expectedStopAt, stolenTask.Offset)
	}
	if stolenTask.Length != (largeSize - expectedStopAt) {
		t.Errorf("Stolen task length should be %d, got %d", largeSize-expectedStopAt, stolenTask.Length)
	}
}

func TestStealWork_RaceConditionSimulation(t *testing.T) {
	// Simulate case where worker advances WHILE stealing happens
	downloader := &ConcurrentDownloader{
		activeTasks: make(map[int]*ActiveTask),
	}
	queue := NewTaskQueue()

	largeSize := int64(100 * 1024 * 1024)
	largeTask := &ActiveTask{
		Task: types.Task{Offset: 0, Length: largeSize},
	}
	atomic.StoreInt64(&largeTask.CurrentOffset, 0)
	atomic.StoreInt64(&largeTask.StopAt, largeSize)

	downloader.activeTasks[1] = largeTask
	advancedOffset := int64(90 * 1024 * 1024)
	atomic.StoreInt64(&largeTask.CurrentOffset, advancedOffset)

	success := downloader.StealWork(queue)
	if !success {
		t.Error("Should still steal if enough remaining")
	}

	// Remaining: 10MB. Split: 5MB.
	// StopAt should become 90+5 = 95MB.
	expectedStopAt := advancedOffset + (largeSize-advancedOffset)/2

	if atomic.LoadInt64(&largeTask.StopAt) != expectedStopAt {
		t.Errorf("StopAt mismatch. Got %d, want %d", atomic.LoadInt64(&largeTask.StopAt), expectedStopAt)
	}
}
