package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/tui/colors"
)

// ChunkMapModel visualizes download chunks as a grid using a bitmap
type ChunkMapModel struct {
	Bitmap          []byte
	BitmapWidth     int // Total number of chunks in bitmap
	Width           int // UI render width (columns * 2)
	Height          int // Available height in rows (0 = auto)
	Paused          bool
	TotalSize       int64
	ActualChunkSize int64
	ChunkProgress   []int64
}

// NewChunkMapModel creates a new chunk map visualization
func NewChunkMapModel(bitmap []byte, bitmapWidth int, width, height int, paused bool, totalSize int64, actualChunkSize int64, chunkProgress []int64) ChunkMapModel {
	return ChunkMapModel{
		Bitmap:          bitmap,
		BitmapWidth:     bitmapWidth,
		Width:           width,
		Height:          height,
		Paused:          paused,
		TotalSize:       totalSize,
		ActualChunkSize: actualChunkSize,
		ChunkProgress:   chunkProgress,
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

	// Use provided height for rows (screen-based sizing)
	// Grid size is ONLY based on screen dimensions, not file chunk count
	targetRows := m.Height
	if targetRows <= 0 {
		// Default to reasonable height if not provided
		targetRows = 4
	}
	// Minimum 3 lines, maximum 5
	if targetRows < 3 {
		targetRows = 3
	}
	if targetRows > 5 {
		targetRows = 5
	}
	targetChunks := targetRows * cols

	// Downsample logic
	visualChunks := make([]types.ChunkStatus, targetChunks)

	// We calculate progress based on byte ranges of the visual blocks
	bytesPerBlock := float64(m.TotalSize) / float64(targetChunks)

	for i := 0; i < targetChunks; i++ {
		// Calculate byte range for this visual block
		blockStartByte := int64(float64(i) * bytesPerBlock)
		blockEndByte := int64(float64(i+1) * bytesPerBlock)
		if blockEndByte > m.TotalSize {
			blockEndByte = m.TotalSize
		}

		blockSize := blockEndByte - blockStartByte
		if blockSize <= 0 {
			visualChunks[i] = types.ChunkPending
			continue
		}

		// Find which source chunks overlap with this range
		// chunkIndex = byteOffset / ActualChunkSize
		startChunkIdx := int(blockStartByte / m.ActualChunkSize)
		endChunkIdx := int((blockEndByte - 1) / m.ActualChunkSize)

		if startChunkIdx < 0 {
			startChunkIdx = 0
		}
		if endChunkIdx >= m.BitmapWidth {
			endChunkIdx = m.BitmapWidth - 1
		}

		// Calculate total downloaded bytes within this visual block
		var downloadedInBlock int64
		allCompleted := true

		for cIdx := startChunkIdx; cIdx <= endChunkIdx; cIdx++ {
			chunkStartByte := int64(cIdx) * m.ActualChunkSize
			chunkEndByte := chunkStartByte + m.ActualChunkSize
			// Clamp to TotalSize for the last chunk
			// (Use m.TotalSize logic or implied CheckChunk logic, simpler to use logic)

			state := m.getChunkState(cIdx)
			if state != types.ChunkCompleted {
				allCompleted = false
			}

			// Intersection of Chunk and VisualBlock
			intersectStart := blockStartByte
			if chunkStartByte > intersectStart {
				intersectStart = chunkStartByte
			}

			intersectEnd := blockEndByte
			if chunkEndByte < intersectEnd {
				intersectEnd = chunkEndByte
			}

			overlap := intersectEnd - intersectStart
			if overlap <= 0 {
				continue
			}

			// Determine how many bytes of this chunk are downloaded
			// Check simple state first
			// state was already fetched above
			switch state {
			case types.ChunkCompleted:
				downloadedInBlock += overlap
			case types.ChunkDownloading:
				// Partial chunk logic
				// If we have progress data, use it for granular rendering
				if len(m.ChunkProgress) > cIdx {
					// We assume bytes assume filled from the start of the chunk
					// downloadedBytes := m.ChunkProgress[cIdx]
					// validRange: [chunkStartByte, chunkStartByte + downloadedBytes)

					validEndByte := chunkStartByte + m.ChunkProgress[cIdx]

					// Calculate overlap of [intersectStart, intersectEnd) with [chunkStartByte, validEndByte)
					// Since chunkStartByte <= intersectStart (mostly), we focus on the end.

					validIntersectEnd := intersectEnd
					if validEndByte < validIntersectEnd {
						validIntersectEnd = validEndByte
					}

					validOverlap := validIntersectEnd - intersectStart
					if validOverlap > 0 {
						downloadedInBlock += validOverlap
					}
				} else {
					// No granular progress data (e.g. Remote TUI), but chunk is marked Downloading
					// Assume full benefit of doubt for visualization - render as downloading
					// treat as if we have some bytes
					downloadedInBlock += 1
				}
			}
		}

		// Determine Status
		if allCompleted {
			visualChunks[i] = types.ChunkCompleted
		} else if downloadedInBlock > 0 {
			// If we have ANY bytes in this visual block, it is "Downloading" (or Paused Partial)
			// This creates the "granular progress" we want.
			visualChunks[i] = types.ChunkDownloading
		} else {
			visualChunks[i] = types.ChunkPending
		}
	}

	var s strings.Builder

	// Styles
	pendingStyle := lipgloss.NewStyle().Foreground(colors.DarkGray)           // Dark gray
	downloadingStyle := lipgloss.NewStyle().Foreground(colors.NeonPink)       // Neon Pink
	pausedStyle := lipgloss.NewStyle().Foreground(colors.StatePaused)         // Yellow/Gold for paused Partial
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
			if m.Paused {
				s.WriteString(pausedStyle.Render(block))
			} else {
				s.WriteString(downloadingStyle.Render(block))
			}
		default: // ChunkPending
			s.WriteString(pendingStyle.Render(block))
		}
	}

	return s.String()
}

// CalculateHeight returns the number of lines needed to render the chunks
// Takes available height to support dynamic sizing
func CalculateHeight(count int, width int, availableHeight int) int {
	if count == 0 {
		return 0
	}
	// Use all available height (screen-based sizing)
	// Minimum 3, maximum 5 for compact look
	rows := availableHeight
	if rows < 3 {
		rows = 3
	}
	if rows > 5 {
		rows = 5
	}
	return rows
}
