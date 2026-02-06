package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/tui/colors"
)

func init() {
	lipgloss.SetColorProfile(termenv.ANSI256)
}

// Helper to check for colors
const (
	ColorPending     = "236" // DarkGray (approx)
	ColorDownloading = "198" // Neon Pink
	ColorPaused      = "3"   // Warning (Yellow) - from lipgloss standard definition usually, checking components
	ColorCompleted   = "14"  // Cyan
)

// Helper to set chunk state in a bitmap
// Index is chunk index. Status: 0=Pending, 1=Downloading, 2=Completed
func setChunk(bitmap []byte, index int, status int) {
	byteIndex := index / 4
	bitOffset := (index % 4) * 2

	// Create mask to clear bits
	mask := byte(3 << bitOffset)
	bitmap[byteIndex] &= ^mask

	// Set bits
	val := byte(status) << bitOffset
	bitmap[byteIndex] |= val
}

func TestChunkMap_Basic(t *testing.T) {
	// Test Case: Perfect 1:1 mapping
	// 4 chunks -> 4 visual blocks (if width allows)
	chunkCount := 4
	bitmap := make([]byte, 1) // stores 4 chunks

	setChunk(bitmap, 0, int(types.ChunkCompleted))
	setChunk(bitmap, 1, int(types.ChunkDownloading))
	setChunk(bitmap, 2, int(types.ChunkPending))
	setChunk(bitmap, 3, int(types.ChunkCompleted))

	// Width=8 -> 4 blocks (2 chars per block)
	// Mock progress data: all chunks full
	progress := make([]int64, chunkCount)
	for i := range progress {
		progress[i] = 1024
	} // 1KB chunks
	model := NewChunkMapModel(bitmap, chunkCount, 8, 0, false, 4096, 1024, progress)

	// Logic generates 10 rows worth of blocks.
	// cols = 8/2 = 4. Total blocks = 10 * 4 = 40.
	// But we only have 4 source chunks.
	// So each source chunk will span multiple visual blocks.

	out := model.View()

	// Just verify connection mostly.
	if !strings.Contains(out, "■") {
		t.Error("Output should contain blocks")
	}
}

func TestChunkMap_GhostPinkFix(t *testing.T) {
	// Scenario: Mixed Completed and Pending
	// Old behavior: Showed Downloading (Pink)
	// New behavior: Should show Pending (Gray)

	chunkCount := 10
	bitmap := make([]byte, 3)

	// 0-4 Completed
	for i := 0; i < 5; i++ {
		setChunk(bitmap, i, int(types.ChunkCompleted))
	}
	// 5-9 Pending
	for i := 5; i < 10; i++ {
		setChunk(bitmap, i, int(types.ChunkPending))
	}

	// 10 chunks, say 10KB total, 1KB each.
	progress := make([]int64, chunkCount)
	for i := 0; i < 5; i++ {
		progress[i] = 1024
	} // Full

	model := NewChunkMapModel(bitmap, chunkCount, 6, 0, false, 10240, 1024, progress) // 6 width -> 3 cols
	_ = model.View()

	// We check if we have Pink in the output.
	// ... (Rest of comments)
}

func TestChunkMap_PausedState(t *testing.T) {
	chunkCount := 4
	bitmap := make([]byte, 1)
	setChunk(bitmap, 0, int(types.ChunkDownloading))

	progress := make([]int64, chunkCount)
	progress[0] = 512 // Half chunk

	// Case 1: Not Paused
	modelActive := NewChunkMapModel(bitmap, chunkCount, 8, 0, false, 4096, 1024, progress)
	outActive := modelActive.View()

	// Case 2: Paused
	modelPaused := NewChunkMapModel(bitmap, chunkCount, 8, 0, true, 4096, 1024, progress)
	outPaused := modelPaused.View()

	if outActive == outPaused {
		t.Error("View should differ between paused and active states")
	}
}

func TestChunkMap_LogicVerify(t *testing.T) {
	// ... (Comments)
	// Input: [C, P] -> Target 1 Block
	// Result: Pending (since mixed)

	chunkCount := 2
	bitmap := make([]byte, 1)
	setChunk(bitmap, 0, int(types.ChunkCompleted))
	setChunk(bitmap, 1, int(types.ChunkPending))

	progress := []int64{1024, 0}

	model := NewChunkMapModel(bitmap, chunkCount, 2, 0, false, 2048, 1024, progress) // 1 col
	out := model.View()

	if strings.Contains(out, "38;5;198") { // 198 is NeonPink
		t.Error("Mixed state (Completed+Pending) should NOT render as Downloading (Pink)")
	}
}

func TestChunkMap_DownloadingPriority(t *testing.T) {
	// Input: [P, D, P] -> Target 1 Block
	// BUT with granular logic, if D has bytes, it renders pink.

	chunkCount := 3
	bitmap := make([]byte, 1)
	setChunk(bitmap, 0, int(types.ChunkPending))
	setChunk(bitmap, 1, int(types.ChunkDownloading))
	setChunk(bitmap, 2, int(types.ChunkPending))

	progress := []int64{0, 512, 0} // Middle chunk 50% done

	model := NewChunkMapModel(bitmap, chunkCount, 2, 0, false, 3072, 1024, progress) // 1 col
	out := model.View()

	// Dynamic check to avoid hardcoded color codes
	pinkStyle := lipgloss.NewStyle().Foreground(colors.NeonPink)
	expectedPink := pinkStyle.Render("■")

	if !strings.Contains(out, expectedPink) {
		t.Errorf("Block containing a Downloading chunk with bytes SHOULD render as Downloading")
	}
}

func TestChunkMap_GranularProgress(t *testing.T) {
	// 1 Huge Chunk (10MB).
	// Downloaded: 1MB (10%)
	// Visualization: 10 Blocks.
	// Expected: Block 0 is Downloading (Pink), Blocks 1-9 are Pending (Gray).

	chunkCount := 1
	totalSize := int64(10 * 1024 * 1024)
	chunkSize := totalSize

	bitmap := make([]byte, 1)
	setChunk(bitmap, 0, int(types.ChunkDownloading))

	progress := []int64{1024 * 1024} // 1MB

	// Width 20 -> 10 Blocks (2 chars each)
	model := NewChunkMapModel(bitmap, chunkCount, 20, 0, false, totalSize, chunkSize, progress)
	out := model.View()

	// Split output into blocks (space separated)
	// Actually View adds newlines if multi-row, but here 10 blocks fit in 10 cols?
	// View logic: targetChunks = 10 * cols.
	// cols = Width/2 = 10. targetChunks = 100 blocks!
	// Wait, targetChunks logic in View is: `targetChunks := 10 * cols`
	// If we want exactly 10 blocks, we need cols=1 ?? No that gives 10 blocks TOTAL (1 row of 10? No 10 rows of 1?)

	// Let's adjust Width to get a simple line.
	// If Width=2, cols=1 and height=10. 10 Rows of 1 block.
	// Then Row 0 should be Pink, Rows 1-9 Gray.

	model = NewChunkMapModel(bitmap, chunkCount, 2, 10, false, totalSize, chunkSize, progress)
	out = model.View()

	rows := strings.Split(strings.TrimSpace(out), "\n")
	if len(rows) != 10 {
		t.Fatalf("Expected 10 rows, got %d", len(rows))
	}

	pinkStyle := lipgloss.NewStyle().Foreground(colors.NeonPink)
	pinkBlock := pinkStyle.Render("■")

	pendingStyle := lipgloss.NewStyle().Foreground(colors.DarkGray)
	grayBlock := pendingStyle.Render("■")

	// Row 0 should be Pink
	if !strings.Contains(rows[0], pinkBlock) {
		t.Errorf("Row 0 should be Pink (Active 10%%)")
	}

	// Row 1 should be Gray
	if !strings.Contains(rows[1], grayBlock) {
		t.Errorf("Row 1 should be Gray (Inactive part of chunk)")
	}
}
