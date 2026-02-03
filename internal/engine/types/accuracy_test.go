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

func TestRestoreBitmap(t *testing.T) {
	state := types.NewProgressState("test-restore", 100*1024*1024) // 100MB

	// Create a bitmap manually
	// 100MB / 1MB chunks = 100 chunks.
	// 2 bits per chunk -> 200 bits -> 25 bytes.
	bitmap := make([]byte, 25)

	// Mark chunk 0 as Completed (10 -> 2)
	// Byte 0: 00 00 00 10 = 0x02 (if index 0 is first 2 bits)
	// Logic is: (status << bitOffset). Index 0 -> Offset 0.
	// val = 2 << 0 = 2.
	bitmap[0] = 0x02

	// Restore
	state.RestoreBitmap(bitmap, 1024*1024) // 1MB chunk size

	// Verify
	if state.ActualChunkSize != 1024*1024 {
		t.Errorf("Expected ActualChunkSize 1MB, got %d", state.ActualChunkSize)
	}

	if state.BitmapWidth != 100 {
		t.Errorf("Expected BitmapWidth 100, got %d", state.BitmapWidth)
	}

	if state.GetChunkState(0) != types.ChunkCompleted {
		t.Errorf("Expected chunk 0 to be completed")
	}

	if state.GetChunkState(1) != types.ChunkPending {
		t.Errorf("Expected chunk 1 to be pending")
	}
}
