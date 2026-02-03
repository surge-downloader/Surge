package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/tui/colors"
)

// ChunkMapModel visualizes download chunks as a grid using a bitmap
type ChunkMapModel struct {
	Bitmap      []byte
	BitmapWidth int // Total number of chunks in bitmap
	Width       int // UI render width (columns * 2)
}

// NewChunkMapModel creates a new chunk map visualization
func NewChunkMapModel(bitmap []byte, bitmapWidth int, width int) ChunkMapModel {
	return ChunkMapModel{
		Bitmap:      bitmap,
		BitmapWidth: bitmapWidth,
		Width:       width,
	}
}

func (m ChunkMapModel) getChunkState(index int) types.ChunkStatus {
	if index < 0 || index >= m.BitmapWidth {
		return types.ChunkPending
	}
	byteIndex := index / 4
	bitOffset := (index % 4) * 2
	if byteIndex >= len(m.Bitmap) {
		return types.ChunkPending
	}
	val := (m.Bitmap[byteIndex] >> bitOffset) & 3
	return types.ChunkStatus(val)
}

// View renders the chunk grid
func (m ChunkMapModel) View() string {
	if m.BitmapWidth == 0 || len(m.Bitmap) == 0 {
		return ""
	}

	// Calculate available width for block rendering
	// We use 2 chars per block (char + space)
	cols := m.Width / 2
	if cols < 1 {
		cols = 1
	}

	// Target 10 rows to maintain the "full grid" look requested
	// 5 * Width is roughly 10 rows (since 1 row = Width / 2 blocks)
	// targetChunks := 5 * m.Width
	// More precisely: 10 * cols
	targetChunks := 10 * cols

	// Downsample logic
	visualChunks := make([]types.ChunkStatus, targetChunks)
	sourceLen := m.BitmapWidth

	for i := 0; i < targetChunks; i++ {
		// Map target index i to source range [start, end)
		// Use float math for even distribution
		start := int(float64(i) * float64(sourceLen) / float64(targetChunks))
		end := int(float64(i+1) * float64(sourceLen) / float64(targetChunks))
		if end > sourceLen {
			end = sourceLen
		}
		if start >= end {
			start = end - 1
			if start < 0 {
				start = 0
			}
		}

		// Determine status for this visual block based on source range
		// Logic:
		// - If all source blocks are Completed -> Completed
		// - If any source block is Downloading/Pending -> Active/Pending logic
		// - Optimistic: If any is Downloading, show Downloading.
		// - If mixed Completed/Pending (no Downloading), show Downloading (active region).
		// - Only show Pending if ALL are Pending.

		allCompleted := true
		anyDownloading := false
		anyCompleted := false

		for j := start; j < end; j++ {
			s := m.getChunkState(j)

			if s != types.ChunkCompleted {
				allCompleted = false
			} else {
				anyCompleted = true
			}
			if s == types.ChunkDownloading {
				anyDownloading = true
			}
		}

		if allCompleted {
			visualChunks[i] = types.ChunkCompleted
		} else if anyDownloading {
			visualChunks[i] = types.ChunkDownloading
		} else if anyCompleted {
			// Mixed Completed/Pending -> Treat as Downloading (active region)
			visualChunks[i] = types.ChunkDownloading
		} else {
			visualChunks[i] = types.ChunkPending
		}
	}

	var s strings.Builder

	// Styles
	pendingStyle := lipgloss.NewStyle().Foreground(colors.DarkGray)           // Dark gray
	downloadingStyle := lipgloss.NewStyle().Foreground(colors.NeonPink)       // Neon Pink
	completedStyle := lipgloss.NewStyle().Foreground(colors.StateDownloading) // Neon Green / Cyan

	block := "■"

	for i, status := range visualChunks {
		if i > 0 && i%cols == 0 {
			s.WriteRune('\n')
		} else if i > 0 {
			s.WriteRune(' ')
		}

		switch status {
		case types.ChunkCompleted:
			s.WriteString(completedStyle.Render(block))
		case types.ChunkDownloading:
			s.WriteString(downloadingStyle.Render(block))
		default: // ChunkPending
			s.WriteString(pendingStyle.Render(block))
		}
	}

	return s.String()
}

// CalculateHeight returns the number of lines needed to render the chunks
func CalculateHeight(count int, width int) int {
	if count == 0 {
		return 0
	}
	return 5
}
