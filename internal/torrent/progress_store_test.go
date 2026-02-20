package torrent

import (
	"os"
	"testing"

	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestProgressStore_ChunkMapIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "surge-test-progressstore")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	info := Info{
		Name:        "test.bin",
		PieceLength: 1024 * 1024,     // 1MB chunks
		Length:      5 * 1024 * 1024, // 5 pieces total
	}

	layout, err := NewFileLayout(tempDir, info)
	if err != nil {
		t.Fatal(err)
	}
	defer layout.Close()

	state := types.NewProgressState("test-progress", info.Length)

	store := NewProgressStore(layout, state)

	// Ensure State initializes correctly inside NewProgressStore
	if state.BitmapWidth != 5 {
		t.Errorf("Expected GUI BitmapWidth 5, got %d", state.BitmapWidth)
	}

	// 1. Write an incoming piece over the network
	data := make([]byte, 512*1024) // 512KB payload dynamically streams in
	err = store.WriteAtPiece(1, 0, data)
	if err != nil {
		t.Fatalf("Failed to write mock data: %v", err)
	}

	// Check that the chunk is now Downloading graphically
	if st := state.GetChunkState(1); st != types.ChunkDownloading {
		t.Errorf("Expected chunk state Downloading, got %v", st)
	}

	// Ensure bytes populated dynamically without global corruption
	bm, width, totalSize, chunkSize, chunkProgress := state.GetBitmap()
	if width != 5 || totalSize != info.Length || chunkSize != info.PieceLength {
		t.Errorf("Bitmap initialization bounds invalid")
	}

	if len(chunkProgress) != 5 {
		t.Fatalf("Chunk progress len mismatch")
	}

	if chunkProgress[1] != int64(len(data)) {
		t.Errorf("Expected chunk UI offset %d, got %d", len(data), chunkProgress[1])
	}

	// 2. A Verification hook occurs when piece downloads 100%
	store.VerifyPieceData(1, make([]byte, 1024*1024)) // Force completion

	store.markPieceVerified(1)

	// After piece verified globally, ChunkMap completes shading gracefully
	if st := state.GetChunkState(1); st != types.ChunkCompleted {
		t.Errorf("Expected chunk state Completed, got %v", st)
	}

	bm, _, _, _, chunkProgress = state.GetBitmap()
	if chunkProgress[1] != info.PieceLength {
		t.Errorf("Expected chunk UI 100-percent completed value (%d), got %d", info.PieceLength, chunkProgress[1])
	}
	_ = bm // Just ensuring compile compliance
}
