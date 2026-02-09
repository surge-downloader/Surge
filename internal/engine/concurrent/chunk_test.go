package concurrent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestTaskRangeAssignment(t *testing.T) {
	runtime := &types.RuntimeConfig{
		MinChunkSize: 1 * types.MB,
	}

	d := &ConcurrentDownloader{
		Runtime: runtime,
	}

	tests := []struct {
		name      string
		fileSize  int64
		numConns  int
		wantChunk int64
		wantTasks int
	}{
		{
			name:      "Exact division",
			fileSize:  100 * types.MB,
			numConns:  4,
			wantChunk: 25 * types.MB,
			wantTasks: 4,
		},
		{
			name:     "Uneven division",
			fileSize: 100*types.MB + 123, // clearly not divisible by 4 aligned
			numConns: 4,
			// 100MB / 4 = 25MB. 123 bytes remainder.
			// Calculation: (104857600 + 123) / 4 = 26214430.
			// Aligned: 26214430 / 4096 * 4096 = 26214400 (25MB).
			// So chunk size is 25MB.
			// 4 tasks of 25MB = 100MB.
			// Remainder 123 bytes -> 5th task.
			wantChunk: 25 * types.MB,
			wantTasks: 5,
		},
		{
			name:      "Small file",
			fileSize:  10 * types.MB,
			numConns:  2,
			wantChunk: 5 * types.MB,
			wantTasks: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunkSize := d.calculateChunkSize(tt.fileSize, tt.numConns)

			// Verify chunk size is close to expected (allow for alignment)
			assert.InDelta(
				t,
				tt.wantChunk,
				chunkSize,
				float64(types.AlignSize),
				"Chunk size mismatch",
			)

			// specific verification for task creation
			tasks := createTasks(tt.fileSize, chunkSize)
			assert.Equal(t, tt.wantTasks, len(tasks), "Task count mismatch")

			// Verify task continuity
			var total int64
			for i, task := range tasks {
				assert.Equal(t, total, task.Offset, "Task offset mismatch at index %d", i)
				total += task.Length
			}
			assert.Equal(t, tt.fileSize, total, "Total task length mismatch")
		})
	}
}
