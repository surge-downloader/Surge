package concurrent

import (
	"sync"
	"sync/atomic"

	"github.com/surge-downloader/surge/internal/engine/types"
)

// TaskQueue is a thread-safe work-stealing queue
type TaskQueue struct {
	tasks       []types.Task
	head        int
	mu          sync.Mutex
	cond        *sync.Cond
	done        bool
	idleWorkers int64 // Atomic counter for idle workers
}

func NewTaskQueue() *TaskQueue {
	tq := &TaskQueue{}
	tq.cond = sync.NewCond(&tq.mu)
	return tq
}

func (q *TaskQueue) Push(t types.Task) {
	q.mu.Lock()
	q.tasks = append(q.tasks, t)
	q.cond.Signal()
	q.mu.Unlock()
}

func (q *TaskQueue) PushMultiple(tasks []types.Task) {
	q.mu.Lock()
	q.tasks = append(q.tasks, tasks...)
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *TaskQueue) Pop() (types.Task, bool) {
	// Mark as idle while waiting
	atomic.AddInt64(&q.idleWorkers, 1)

	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.tasks) == 0 && !q.done {
		q.cond.Wait()
	}

	// No longer idle once we have work (or are done)
	atomic.AddInt64(&q.idleWorkers, -1)

	if len(q.tasks) == 0 {
		return types.Task{}, false
	}

	t := q.tasks[q.head]
	q.head++
	if q.head > len(q.tasks)/2 {

		// slice instead of copy to avoid allocation
		q.tasks = q.tasks[q.head:]
		q.head = 0
	}
	return t, true
}

func (q *TaskQueue) Close() {
	q.mu.Lock()
	q.done = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *TaskQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks) - q.head
}

func (q *TaskQueue) IdleWorkers() int64 {
	return atomic.LoadInt64(&q.idleWorkers)
}

// DrainRemaining returns all remaining tasks in the queue (used for pause/resume)
func (q *TaskQueue) DrainRemaining() []types.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.head >= len(q.tasks) {
		return nil
	}

	remaining := make([]types.Task, len(q.tasks)-q.head)
	copy(remaining, q.tasks[q.head:])
	q.tasks = nil
	q.head = 0
	return remaining
}
