package types_test

import (
	"testing"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestChunkAccuracy(t *testing.T) {
	state := types.NewProgressState("test", 100*1024*1024) // 100MB

	// Init 200 chunks -> 500KB per chunk
	state.InitChunks(100 * 1024 * 1024)

	// Simulate downloading a small part of the first chunk (e.g. 1KB)
	// UpdateChunkStatus(offset=0, length=1024, status=ChunkCompleted)
	state.UpdateChunkStatus(0, 1024, types.ChunkCompleted)

	chunks := state.GetChunks()
	if chunks[0] == types.ChunkCompleted {
		t.Error("FAIL: 1KB download marked 500KB visual chunk as Completed")
	} else if chunks[0] == types.ChunkDownloading {
		t.Log("PASS: Chunk marked as Downloading (partial progress)")
	} else {
		t.Error("FAIL: Chunk status unexpected:", chunks[0])
	}

	// Calculate percentage
	completed := 0
	for _, c := range chunks {
		if c == types.ChunkCompleted {
			completed++
		}
	}
	t.Logf("Visual Completion: %.2f%%", float64(completed)/float64(len(chunks))*100)
}
