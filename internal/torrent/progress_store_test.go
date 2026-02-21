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

func TestProgressStore_TorrentResumeRestoresVisuals(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "surge-test-progressstore-resume")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	info := Info{
		Name:        "resume.bin",
		PieceLength: 512 * 1024,
		Length:      2 * 1024 * 1024, // 2MB total, 4 pieces
	}
	layout, err := NewFileLayout(tempDir, info)
	if err != nil {
		t.Fatalf("failed to create layout: %v", err)
	}
	defer layout.Close()

	// Create fake chunk bitmap showing pieces 0 and 2 are complete
	// 4 chunks total, represented in 1 byte (2 bits per chunk)
	state := types.NewProgressState("resumetest", layout.TotalLength)
	state.InitBitmap(layout.TotalLength, layout.Info.PieceLength)

	// chunk 0 operates on bits 0-1 (value 2 -> 0x02)
	// chunk 1 operates on bits 2-3 (value 0 -> 0x00)
	// chunk 2 operates on bits 4-5 (value 2 -> 0x20)
	// chunk 3 operates on bits 6-7 (value 0 -> 0x00)
	// Total: 0x22
	mockBitmap := []byte{0x22}
	state.RestoreBitmap(mockBitmap, layout.Info.PieceLength)

	// Initialize the progress store with the mocked restored state
	store := NewProgressStore(layout, state)

	if !store.verified[0] {
		t.Errorf("expected piece 0 to be marked verified in store based on bitmap")
	}
	if store.verified[1] {
		t.Errorf("expected piece 1 to NOT be marked verified")
	}
	if !store.verified[2] {
		t.Errorf("expected piece 2 to be marked verified in store based on bitmap")
	}
	if store.verified[3] {
		t.Errorf("expected piece 3 to NOT be marked verified")
	}

	// Verify the ProgressState array visually reflects full piece size without global byte counter increase
	if state.ChunkProgress[0] != layout.Info.PieceLength {
		t.Errorf("expected visual ChunkProgress[0] to be full piece size %d, got %d", layout.Info.PieceLength, state.ChunkProgress[0])
	}
	if state.ChunkProgress[1] != 0 {
		t.Errorf("expected visual ChunkProgress[1] to be 0, got %d", state.ChunkProgress[1])
	}
	if state.ChunkProgress[2] != layout.Info.PieceLength {
		t.Errorf("expected visual ChunkProgress[2] to be full piece size %d, got %d", layout.Info.PieceLength, state.ChunkProgress[2])
	}
	if state.ChunkProgress[3] != 0 {
		t.Errorf("expected visual ChunkProgress[3] to be 0, got %d", state.ChunkProgress[3])
	}
}
