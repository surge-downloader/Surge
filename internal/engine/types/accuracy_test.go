package types_test

import (
	"testing"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestChunkAccuracy(t *testing.T) {
	state := types.NewProgressState("test", 100*1024*1024) // 100MB

	// Init 200 chunks -> 500KB per chunk
	// 10 MB total, 1 MB chunks
	state.InitBitmap(10*1024*1024, 1024*1024)

	// Simulate downloading a small part of the first chunk (e.g. 1KB)
	// UpdateChunkStatus(offset=0, length=1024, status=ChunkCompleted)
	// Update first 500KB (half of first chunk)
	state.UpdateChunkStatus(0, 500*1024, types.ChunkDownloading)

	// Verify
	if state.GetChunkState(0) != types.ChunkDownloading {
		t.Errorf("Expected chunk 0 to be Downloading")
	}

	// Calculate percentage
	// Calculate visual percentage
	activeCount := 0
	bitmap, width := state.GetBitmap()

	// Helpers to decode bitmap manually for test verification
	getComp := func(idx int) bool {
		byteIndex := idx / 4
		bitOffset := (idx % 4) * 2
		val := (bitmap[byteIndex] >> bitOffset) & 3
		return types.ChunkStatus(val) == types.ChunkDownloading || types.ChunkStatus(val) == types.ChunkCompleted
	}

	for i := 0; i < width; i++ {
		if getComp(i) {
			activeCount++
		}
	}

	pct := float64(activeCount) / float64(width)

	// We expect 1 chunk out of 10 to be active (10%)
	if pct < 0.09 || pct > 0.11 {
		t.Errorf("Expected ~10%% visual activity (1 chunk active), got %.2f%%", pct*100)
	}
	t.Logf("Visual Completion: %.2f%%", pct*100)
}
