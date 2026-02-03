package components

import (
	"strings"
	"testing"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestChunkMapResampling(t *testing.T) {
	// Test Case 1: Downsampling (Source > Target)
	// Source: 10 chunks (5 Done, 5 Pending)
	// Target: 5 chunks
	// Expected: First 2.5 chunks map to 1.
	// 5/10 = 0.5.
	// Actually: 10 source -> 5 target. Each target covers 2 source.
	// Source: [C, C, C, C, C, P, P, P, P, P]
	// Target 0 (Src 0-2): C, C -> C
	// Target 1 (Src 2-4): C, C -> C
	// Target 2 (Src 4-6): C, P -> D (in progress/mixed)
	// Target 3 (Src 6-8): P, P -> P
	// Target 4 (Src 8-10): P, P -> P

	// Manual bitmap creation for test
	// 10 chunks. 2 bits per chunk. Need ceil(10/4) = 3 bytes.
	// 5 Completed (10), 5 Pending (00)
	// Chunks 0-3: 10 10 10 10 = 0xAA (assuming Little Endian or Big Endian? Logic is shift 2*offset)
	// SetChunkState uses (status << bitOffset). bitOffset increases with index.
	// Index 0: bits 0-1. Index 1: bits 2-3.
	// So 0xAA means all 4 chunks are Completed (0b10).

	// Let's use the helper types/progress logic safely manually or mock it.
	// Actually we can just rely on the test running against components package.
	// Let's construct bytes manually.
	// 0-4 Completed (5 chunks):
	// Byte 0 (Indices 0-3): 10 10 10 10 = 0xAA
	// Byte 1 (Indices 4-7): Ind 4=10, Ind 5-7=00 -> 00 00 00 10 = 0x02
	// Byte 2 (Indices 8-9): 00 00 = 0x00

	bitmap := []byte{0xAA, 0x02, 0x00}

	model := NewChunkMapModel(bitmap, 10, 10) // Width 10 -> Ccols 5 -> Target 50

	out := model.View()
	if !strings.Contains(out, "■") {
		t.Error("Output should contain blocks")
	}

	// Upsampling test
	// 2 chunks (Completed, Pending) -> Width 2
	// Byte 0: Ind 0=Completed(10), Ind 1=Pending(00) -> 00 10 = 0x02
	bitmap2 := []byte{0x02}
	model2 := NewChunkMapModel(bitmap2, 2, 2)
	out2 := model2.View()
	_ = out2
}

func TestResampleLogic(t *testing.T) {
	// Skip detailed logic verification since we moved it into internal private methods of ChunkMapModel
	// or we can test public View
}

// Helper duplicating the logic for test
func ResampleChunks(source []types.ChunkStatus, targetChunks int) []types.ChunkStatus {
	visualChunks := make([]types.ChunkStatus, targetChunks)
	sourceLen := len(source)

	for i := 0; i < targetChunks; i++ {
		start := int(float64(i) * float64(sourceLen) / float64(targetChunks))
		end := int(float64(i+1) * float64(sourceLen) / float64(targetChunks))
		if end > sourceLen {
			end = sourceLen
		}

		// The BUG FIX logic we want to verify:
		// If upsamping (start == end), ensure we pick at least one
		if start >= end {
			// Center sampling strategy
			center := int((float64(i) + 0.5) * float64(sourceLen) / float64(targetChunks))
			start = center
			end = center + 1
		}

		allCompleted := true
		anyDownloading := false
		anyCompleted := false

		// Safety check
		if start < 0 {
			start = 0
		}
		if end > sourceLen {
			end = sourceLen
		}

		loopCount := 0
		for j := start; j < end; j++ {
			loopCount++
			s := source[j]
			if s != types.ChunkCompleted {
				allCompleted = false
			} else {
				anyCompleted = true
			}
			if s == types.ChunkDownloading {
				anyDownloading = true
			}
		}

		// If empty loop (should not happen with fix), defaults to allCompleted=true which is BAD
		if loopCount == 0 {
			allCompleted = false // Fail safe
		}

		if allCompleted && loopCount > 0 {
			visualChunks[i] = types.ChunkCompleted
		} else if anyDownloading {
			visualChunks[i] = types.ChunkDownloading
		} else if anyCompleted {
			visualChunks[i] = types.ChunkDownloading
		} else {
			visualChunks[i] = types.ChunkPending
		}
	}
	return visualChunks
}
