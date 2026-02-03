package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// ChunkMapModel visualizes download chunks as a grid
type ChunkMapModel struct {
	Chunks []types.ChunkStatus
	Width  int
	Height int
}

// NewChunkMapModel creates a new chunk map visualization
func NewChunkMapModel(chunks []types.ChunkStatus, width int) ChunkMapModel {
	return ChunkMapModel{
		Chunks: chunks,
		Width:  width,
	}
}

// UpdateChunks updates the chunk data
func (m *ChunkMapModel) UpdateChunks(chunks []types.ChunkStatus) {
	m.Chunks = chunks
}

// View renders the chunk grid
func (m ChunkMapModel) View() string {
	if len(m.Chunks) == 0 {
		return ""
	}

	// Calculate available width for block rendering
	// We use 2 chars per block (char + space)
	cols := m.Width / 2
	if cols < 1 {
		cols = 1
	}

	var s strings.Builder

	// Styles
	pendingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("237"))     // Dark gray
	downloadingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("213")) // Neon Pink
	completedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))    // Neon Green / Cyan (using bright green for high contrast)
	// Alternatively use Cyan "14" or "206"

	block := "■"

	for i, status := range m.Chunks {
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
	cols := width / 2
	if cols < 1 {
		cols = 1
	}
	// ceil(count / cols)
	return (count + cols - 1) / cols
}
