package concurrent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestEndGame_AssignShadowWork(t *testing.T) {
	// Setup
	d := &ConcurrentDownloader{
		activeTasks: make(map[int]*ActiveTask),
	}
	queue := NewTaskQueue()

	// 1. Add an active task (single worker)
	task := types.Task{Offset: 100, Length: 50}
	activeTask := &ActiveTask{
		Task: task,
	}
	d.activeMu.Lock()
	d.activeTasks[1] = activeTask
	d.activeMu.Unlock()

	// 2. Run Shadow Assignment
	d.assignShadowWork(queue)

	// 3. Verify that a shadow task was pushed to queue
	// We check if queue length is 1
	if queue.Len() != 1 {
		t.Fatalf("Expected 1 shadow task in queue, got %d", queue.Len())
	}

	popped, ok := queue.Pop()
	assert.True(t, ok)
	assert.Equal(t, int64(100), popped.Offset)
	assert.Equal(t, int64(50), popped.Length)

	// 4. Add a second worker for the SAME chunks (simulation of race already started)
	d.activeMu.Lock()
	d.activeTasks[2] = &ActiveTask{Task: task}
	d.activeMu.Unlock()

	// 5. Run Shadow Assignment again
	d.assignShadowWork(queue)

	// 6. Verify NO new shadow task (count is already 2)
	assert.Equal(t, 0, queue.Len(), "Should not assign shadow if 2 workers already valid")
}

func TestEndGame_CleanUpShadows(t *testing.T) {
	d := &ConcurrentDownloader{
		activeTasks: make(map[int]*ActiveTask),
	}

	// Two workers racing on same offset
	offset := int64(500)

	ctx1, cancel1 := context.WithCancel(context.Background())
	task1 := &ActiveTask{
		Task:   types.Task{Offset: offset},
		Cancel: cancel1,
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	task2 := &ActiveTask{
		Task:   types.Task{Offset: offset},
		Cancel: cancel2,
	}

	// Unrelated task
	ctx3, cancel3 := context.WithCancel(context.Background())
	task3 := &ActiveTask{
		Task:   types.Task{Offset: 999},
		Cancel: cancel3,
	}

	d.activeMu.Lock()
	d.activeTasks[1] = task1
	d.activeTasks[2] = task2
	d.activeTasks[3] = task3
	d.activeMu.Unlock()

	// Winner is Worker 1
	d.cleanUpShadows(offset, 1)

	// Worker 2 (Loser) should be cancelled
	select {
	case <-ctx2.Done():
		// Success
	default:
		t.Fatal("Shadow worker (loser) was not cancelled")
	}

	// Worker 1 (Winner) should NOT be cancelled (obviously, but good to check logic safety)
	select {
	case <-ctx1.Done():
		t.Fatal("Winner worker was cancelled incorrectly")
	default:
		// Success
	}

	// Unrelated Worker 3 should NOT be cancelled
	select {
	case <-ctx3.Done():
		t.Fatal("Unrelated worker was cancelled incorrectly")
	default:
		// Success
	}
}
