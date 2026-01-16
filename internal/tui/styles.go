package tui

import (
	"github.com/junaid2005p/surge/internal/tui/colors"

	"github.com/charmbracelet/lipgloss"
)

// Re-export colors from colors package for backward compatibility
var (
	ColorNeonPurple       = colors.NeonPurple
	ColorNeonPink         = colors.NeonPink
	ColorNeonCyan         = colors.NeonCyan
	ColorDarkGray         = colors.DarkGray
	ColorGray             = colors.Gray
	ColorLightGray        = colors.LightGray
	ColorWhite            = colors.White
	ColorStateError       = colors.StateError
	ColorStatePaused      = colors.StatePaused
	ColorStateDownloading = colors.StateDownloading
	ColorStateDone        = colors.StateDone
)

// Progress bar color constants
const (
	ProgressStart = colors.ProgressStart
	ProgressEnd   = colors.ProgressEnd
)

// === Layout Styles ===
var (

	// The main box surrounding everything (optional, depending on terminal size)
	AppStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("0")). // Transparent/Default
			Foreground(ColorWhite)

	// Standard pane border
	PaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorGray).
			Padding(0, 1)

	// Focus style for the active pane
	ActivePaneStyle = PaneStyle.
			BorderForeground(ColorNeonPink)

	// === Specific Component Styles ===

	// 1. The "SURGE" Header
	LogoStyle = lipgloss.NewStyle().
			Foreground(ColorNeonPurple).
			Bold(true).
			MarginBottom(1)

	// 2. The Speed Graph (Top Right)
	GraphStyle = PaneStyle.
			BorderForeground(ColorNeonCyan)

	// 3. The Download List (Bottom Left)
	ListStyle = ActivePaneStyle // Usually focused by default

	// 4. The Detail View (Bottom Right)
	DetailStyle = PaneStyle

	// === Text Styles ===

	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Bold(true).
			MarginBottom(1)

	// Helper for bold titles inside panes
	PaneTitleStyle = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Bold(true)

	TabStyle = lipgloss.NewStyle().
			Foreground(ColorLightGray).
			Padding(0, 1)

	ActiveTabStyle = lipgloss.NewStyle().
			Foreground(ColorNeonPink).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorNeonPink).
			Padding(0, 1).
			Bold(true)

	StatsLabelStyle = lipgloss.NewStyle().
			Foreground(ColorNeonCyan).
			Width(12)

	StatsValueStyle = lipgloss.NewStyle().
			Foreground(ColorNeonPink).
			Bold(true)

	// Log Entry Styles
	LogStyleStarted = lipgloss.NewStyle().
			Foreground(ColorStateDownloading)

	LogStyleComplete = lipgloss.NewStyle().
				Foreground(ColorStateDone)

	LogStyleError = lipgloss.NewStyle().
			Foreground(ColorStateError)

	LogStylePaused = lipgloss.NewStyle().
			Foreground(ColorStatePaused)
)
