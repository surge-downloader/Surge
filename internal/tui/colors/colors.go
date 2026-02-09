package colors

import "github.com/charmbracelet/lipgloss"

// === Color Palette ===
// Vibrant "Cyberpunk" Neon Colors (Dark Mode) + High Contrast (Light Mode)
// [TODO]: Of course this is hardcoded too. Fix the color blasphemy.
// Please next time say your LLM to NOT hard code colors.
var (
	NeonPurple = lipgloss.AdaptiveColor{Light: "#5d40c9", Dark: "#bd93f9"}
	NeonPink   = lipgloss.AdaptiveColor{Light: "#d10074", Dark: "#ff79c6"}
	NeonCyan   = lipgloss.AdaptiveColor{Light: "#0073a8", Dark: "#8be9fd"}
	DarkGray   = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#282a36"} // Background
	Gray       = lipgloss.AdaptiveColor{Light: "#d0d0d0", Dark: "#44475a"} // Borders
	LightGray  = lipgloss.AdaptiveColor{
		Light: "#4a4a4a",
		Dark:  "#a9b1d6",
	} // Brighter text for secondary info
	White = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#f8f8f2"}
)

// === Semantic State Colors ===
var (
	StateError = lipgloss.AdaptiveColor{
		Light: "#d32f2f",
		Dark:  "#ff5555",
	} // 🔴 Red - Error/Stopped
	StatePaused = lipgloss.AdaptiveColor{
		Light: "#f57c00",
		Dark:  "#ffb86c",
	} // 🟡 Orange - Paused/Queued
	StateDownloading = lipgloss.AdaptiveColor{
		Light: "#2e7d32",
		Dark:  "#50fa7b",
	} // 🟢 Green - Downloading
	StateDone = lipgloss.AdaptiveColor{
		Light: "#7b1fa2",
		Dark:  "#bd93f9",
	} // 🔵 Purple - Completed
)

// === Progress Bar Colors ===
var (
	ProgressStart = lipgloss.AdaptiveColor{Light: "#d10074", Dark: "#ff79c6"} // Pink
	ProgressEnd   = lipgloss.AdaptiveColor{Light: "#7b1fa2", Dark: "#bd93f9"} // Purple
)
