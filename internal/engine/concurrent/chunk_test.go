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
			assert.InDelta(t, tt.wantChunk, chunkSize, float64(types.AlignSize), "Chunk size mismatch")

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

func TestCalculateChunkSize_EdgeCases(t *testing.T) {
	// Setup with known min chunk size
	runtime := &types.RuntimeConfig{
		MinChunkSize: 2 * types.MB,
	}
	d := &ConcurrentDownloader{
		Runtime: runtime,
	}

	tests := []struct {
		name      string
		fileSize  int64
		numConns  int
		wantChunk int64
	}{
		{
			name:      "Zero connections (safety check)",
			fileSize:  100 * types.MB,
			numConns:  0,
			wantChunk: 2 * types.MB, // Fallback to MinChunkSize
		},
		{
			name:      "Negative connections (safety check)",
			fileSize:  100 * types.MB,
			numConns:  -5,
			wantChunk: 2 * types.MB, // Fallback to MinChunkSize
		},
		{
			name:      "Chunk size smaller than MinChunkSize (clamping)",
			fileSize:  10 * types.MB,
			numConns:  10,                       // 1MB per conn
			wantChunk: 2 * types.MB,             // Clamped to MinChunkSize (2MB)
		},
		{
			name:      "Chunk size alignment (unaligned division)",
			fileSize:  100*types.MB + 123,       // Not perfectly divisible
			numConns:  4,                        // ~25MB
			wantChunk: 25 * types.MB,            // 25MB is aligned to 4KB
		},
		{
			name:      "Chunk size alignment (force unaligned)",
			// 10MB + 2KB. 2 Conns.
			// (10MB + 2KB) / 2 = 5MB + 1KB.
			// 5MB + 1KB is NOT aligned to 4KB (AlignSize).
			// Should round down to nearest 4KB -> 5MB.
			fileSize:  10*types.MB + 2*types.KB,
			numConns:  2,
			wantChunk: 5 * types.MB,
		},
		{
			name:      "Very small file (less than MinChunkSize)",
			fileSize:  1 * types.MB,
			numConns:  1,
			wantChunk: 2 * types.MB, // Clamped to MinChunkSize
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.calculateChunkSize(tt.fileSize, tt.numConns)
			assert.Equal(t, tt.wantChunk, got, "Chunk size mismatch for %s", tt.name)

			// Verify alignment
			if got > 0 {
				assert.Equal(t, int64(0), got%types.AlignSize, "Chunk size %d is not aligned to %d", got, types.AlignSize)
			}
		})
	}

	// Special case for Zero Chunk Size logic:
	// If MinChunkSize is very small (1 byte), we can test the alignment logic where it rounds down to 0.
	t.Run("Zero chunk size logic", func(t *testing.T) {
		runtimeSmall := &types.RuntimeConfig{
			MinChunkSize: 1, // Set to 1 byte (must be > 0 to avoid default override)
		}
		dSmall := &ConcurrentDownloader{
			Runtime: runtimeSmall,
		}

		// 1KB / 1 = 1KB.
		// 1KB / 4KB = 0 (integer division).
		// 0 * 4KB = 0.
		// Should become 4KB (AlignSize) by the safety check.
		got := dSmall.calculateChunkSize(1*types.KB, 1)
		assert.Equal(t, int64(types.AlignSize), got, "Should be bumped to AlignSize")
	})
}
