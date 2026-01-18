package components

import (
	"github.com/surge-downloader/surge/internal/tui/colors"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// ConfirmationModal renders a styled confirmation dialog box
type ConfirmationModal struct {
	Title       string
	Message     string
	Detail      string         // Optional additional detail line (e.g., filename, URL)
	Keys        help.KeyMap    // Key bindings to show in help
	Help        help.Model     // Help model for rendering keys
	BorderColor lipgloss.Color // Border color for the box
	Width       int
	Height      int
}

// ConfirmationKeyMap defines keybindings for a confirmation modal
type ConfirmationKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
	Extra   key.Binding // Optional extra action (e.g., "Focus Existing")
}

// ShortHelp returns keybindings to show
func (k ConfirmationKeyMap) ShortHelp() []key.Binding {
	if k.Extra.Enabled() {
		return []key.Binding{k.Confirm, k.Extra, k.Cancel}
	}
	return []key.Binding{k.Confirm, k.Cancel}
}

// FullHelp returns keybindings for the expanded help view
func (k ConfirmationKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// NewConfirmationModal creates a modal with default styling
func NewConfirmationModal(title, message, detail string, keys help.KeyMap, helpModel help.Model, borderColor lipgloss.Color) ConfirmationModal {
	return ConfirmationModal{
		Title:       title,
		Message:     message,
		Detail:      detail,
		Keys:        keys,
		Help:        helpModel,
		BorderColor: borderColor,
		Width:       60,
		Height:      10,
	}
}

// View renders the confirmation modal content (without the box wrapper or help text)
func (m ConfirmationModal) View() string {
	detailStyle := lipgloss.NewStyle().
		Foreground(colors.NeonPurple).
		Bold(true)

	// Build content - just message and detail (no help)
	content := m.Message

	if m.Detail != "" {
		content = lipgloss.JoinVertical(lipgloss.Center,
			content,
			"",
			detailStyle.Render(m.Detail),
		)
	}

	return content
}

// RenderWithBtopBox renders the modal using the btop-style box with title in border
// Help text is pushed to the last line of the modal
func (m ConfirmationModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor lipgloss.Color) string,
	titleStyle lipgloss.Style,
) string {
	innerWidth := m.Width - 4 // Account for borders
	innerHeight := m.Height - 2

	// Get content without help
	mainContent := m.View()

	// Style and center help text
	helpStyle := lipgloss.NewStyle().
		Foreground(colors.Gray).
		Width(innerWidth).
		Align(lipgloss.Center)
	helpText := helpStyle.Render(m.Help.View(m.Keys))

	// Calculate heights
	mainContentHeight := lipgloss.Height(mainContent)
	helpHeight := lipgloss.Height(helpText)

	// Space above content to vertically center the main content in remaining space
	remainingHeight := innerHeight - helpHeight - 1 // -1 for spacing before help
	topPadding := (remainingHeight - mainContentHeight) / 2
	if topPadding < 0 {
		topPadding = 0
	}

	// Center main content horizontally
	centeredMain := lipgloss.NewStyle().Width(innerWidth).Align(lipgloss.Center).Render(mainContent)

	// Build final content with help at bottom
	var lines []string
	for i := 0; i < topPadding; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, centeredMain)

	// Add padding to push help to bottom
	spacingNeeded := innerHeight - topPadding - mainContentHeight - helpHeight
	for i := 0; i < spacingNeeded; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, helpText)

	fullContent := lipgloss.JoinVertical(lipgloss.Left, lines...)

	// Title goes in the box border
	return renderBox(titleStyle.Render(" "+m.Title+" "), "", fullContent, m.Width, m.Height, m.BorderColor)
}

// Centered returns the modal centered in the given dimensions (for standalone use)
// Help text is pushed to the last line
func (m ConfirmationModal) Centered(width, height int) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(m.BorderColor).
		Padding(1, 4)

	innerWidth := m.Width - 10 // Account for borders and padding

	// Get content without help
	mainContent := m.View()

	// Style and center help text
	helpStyle := lipgloss.NewStyle().
		Foreground(colors.Gray).
		Width(innerWidth).
		Align(lipgloss.Center)
	helpText := helpStyle.Render(m.Help.View(m.Keys))

	// Full content with spacing to push help down
	fullContent := lipgloss.JoinVertical(lipgloss.Center,
		mainContent,
		"",
		"",
		helpText,
	)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		boxStyle.Render(fullContent))
}
