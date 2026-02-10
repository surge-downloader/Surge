package types

import (
	"testing"
)

func TestReproUpdateChunkStatus(t *testing.T) {
	// Setup
	totalSize := int64(100 * 1024 * 1024) // 100 MB
	chunkSize := int64(10 * 1024 * 1024)  // 10 MB
	ps := NewProgressState("test", totalSize)

	// Init
	ps.InitBitmap(totalSize, chunkSize)

	if ps.ActualChunkSize != chunkSize {
		t.Fatalf("ActualChunkSize expected %d, got %d", chunkSize, ps.ActualChunkSize)
	}

	// 1. Mark first chunk as Downloading
	ps.UpdateChunkStatus(0, chunkSize, ChunkDownloading)

	status := ps.GetChunkState(0)
	if status != ChunkDownloading {
		t.Errorf("Expected Chunk 0 to be Downloading (1), got %v", status)
	}

	// 2. Mark first chunk as Completed (simulate efficient completion)
	// Passing ChunkCompleted triggers the accumulation logic
	ps.UpdateChunkStatus(0, chunkSize, ChunkCompleted)

	status = ps.GetChunkState(0)
	if status != ChunkCompleted {
		t.Errorf("Expected Chunk 0 to be Completed (2), got %v", status)
		t.Errorf("ChunkProgress[0] = %d / %d", ps.ChunkProgress[0], chunkSize)
	}

	// 3. Mark partial download
	offset := int64(chunkSize) // Start of 2nd chunk
	length := int64(1024)      // 1KB
	ps.UpdateChunkStatus(offset, length, ChunkCompleted)

	status = ps.GetChunkState(1)
	if status != ChunkDownloading {
		t.Errorf("Expected Chunk 1 to be Downloading (1) [Partial], got %v", status)
	}

	if ps.ChunkProgress[1] != length {
		t.Errorf("Expected ChunkProgress[1] = %d, got %d", length, ps.ChunkProgress[1])
	}

	// 4. Fill the rest of chunk 1
	remaining := chunkSize - length
	ps.UpdateChunkStatus(offset+length, remaining, ChunkCompleted)

	status = ps.GetChunkState(1)
	if status != ChunkCompleted {
		t.Errorf("Expected Chunk 1 to be Completed (2) [Filled], got %v", status)
		t.Errorf("ChunkProgress[1] = %d / %d", ps.ChunkProgress[1], chunkSize)
	}
}
