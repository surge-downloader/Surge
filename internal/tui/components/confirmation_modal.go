package components

import (
	"github.com/junaid2005p/surge/internal/tui/colors"

	"github.com/charmbracelet/lipgloss"
)

// ConfirmationModal renders a styled confirmation dialog box
type ConfirmationModal struct {
	Title       string
	Message     string
	Detail      string         // Optional additional detail line (e.g., filename, URL)
	HelpText    string         // e.g., "[Y] Yes  [N] No"
	BorderColor lipgloss.Color // Border color for the box
	Width       int
	Height      int
}

// NewConfirmationModal creates a modal with default styling
func NewConfirmationModal(title, message, detail, helpText string) ConfirmationModal {
	return ConfirmationModal{
		Title:       title,
		Message:     message,
		Detail:      detail,
		HelpText:    helpText,
		BorderColor: colors.NeonCyan,
		Width:       60,
		Height:      10,
	}
}

// View renders the confirmation modal content (without the box wrapper)
func (m ConfirmationModal) View() string {
	titleStyle := lipgloss.NewStyle().
		Foreground(colors.NeonCyan).
		Bold(true)

	detailStyle := lipgloss.NewStyle().
		Foreground(colors.NeonPurple).
		Bold(true)

	helpStyle := lipgloss.NewStyle().
		Foreground(colors.LightGray)

	// Build content
	content := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render(m.Title),
		"",
		m.Message,
	)

	if m.Detail != "" {
		content = lipgloss.JoinVertical(lipgloss.Center,
			content,
			"",
			detailStyle.Render(m.Detail),
		)
	}

	content = lipgloss.JoinVertical(lipgloss.Center,
		content,
		"",
		helpStyle.Render(m.HelpText),
	)

	return content
}

// RenderWithBtopBox renders the modal using the btop-style box
// This function should be called from view.go where renderBtopBox is available
func (m ConfirmationModal) RenderWithBtopBox(renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor lipgloss.Color) string) string {
	paddedContent := lipgloss.NewStyle().Padding(1, 2).Render(m.View())
	return renderBox("", "", paddedContent, m.Width, m.Height, m.BorderColor)
}

// Centered returns the modal centered in the given dimensions (for standalone use)
func (m ConfirmationModal) Centered(width, height int) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(m.BorderColor).
		Padding(1, 4)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		boxStyle.Render(m.View()))
}
