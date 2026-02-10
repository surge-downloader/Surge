package concurrent

import (
	"testing"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func BenchmarkTaskQueue_Drain(b *testing.B) {
	b.Run("DrainLargeQueue", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			q := NewTaskQueue()
			// Use a sufficiently large count to trigger multiple resizes if the logic applies recursively,
			// or at least one big resize.
			// With current logic:
			// 10000 -> pop 5001 -> resize to 4999
			// 4999 -> pop 2500 -> resize to 2499
			// ...
			count := 10000
			tasks := make([]types.Task, count)
			q.PushMultiple(tasks)
			b.StartTimer()

			for j := 0; j < count; j++ {
				q.Pop()
			}
		}
	})
}
