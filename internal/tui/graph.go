package tui

import (
	"fmt"
	"strings"

	"github.com/surge-downloader/surge/internal/utils"

	"github.com/charmbracelet/lipgloss"
)

// GraphStats contains the statistics to overlay on the graph
type GraphStats struct {
	DownloadSpeed float64 // Current download speed in MB/s
	DownloadTop   float64 // Top download speed in MB/s
	DownloadTotal int64   // Total downloaded bytes
}

var graphGradient = []lipgloss.TerminalColor{
	lipgloss.AdaptiveColor{Light: "#ce93d8", Dark: "#5f005f"}, // Bottom
	lipgloss.AdaptiveColor{Light: "#ab47bc", Dark: "#8700af"},
	lipgloss.AdaptiveColor{Light: "#8e24aa", Dark: "#af00d7"},
	lipgloss.AdaptiveColor{Light: "#4a148c", Dark: "#ff00ff"}, // Top
}

// renderMultiLineGraph creates a multi-line bar graph with grid lines.
// The graph scales data to fill the full width.
// data: speed history data points
// width, height: dimensions of the graph
// maxVal: maximum value for scaling
// color: color for the data bars
// stats: stats to display in overlay box (pass nil to skip)
func renderMultiLineGraph(data []float64, width, height int, maxVal float64, color lipgloss.TerminalColor, stats *GraphStats) string {
	if width < 1 || height < 1 {
		return ""
	}

	// Styles
	gridStyle := lipgloss.NewStyle().Foreground(ColorGray)
	// barStyle := lipgloss.NewStyle().Foreground(color)

	// 1. Prepare the canvas with a Grid
	rows := make([][]string, height)
	for i := range rows {
		rows[i] = make([]string, width)
		for j := range rows[i] {
			if i == height-1 {
				// Bottom row: solid baseline
				rows[i][j] = gridStyle.Render("─")
			} else if i%2 == 0 {
				rows[i][j] = gridStyle.Render("╌")
			} else {
				rows[i][j] = " "
			}
		}
	}

	// Block characters for partial fills
	blocks := []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

	// Pre-calculate styles for every row to avoid re-creating them in the loop
	// Optimization: Pre-render all possible block characters for each row style
	// This avoids calling style.Render() width*height times
	rowChars := make([][]string, height)
	for y := 0; y < height; y++ {
		// Map height 'y' to an index in graphGradient
		colorIdx := (y * len(graphGradient)) / height
		if colorIdx >= len(graphGradient) {
			colorIdx = len(graphGradient) - 1
		}
		style := lipgloss.NewStyle().Foreground(graphGradient[colorIdx])

		// Pre-render the 8 block characters + space
		rowChars[y] = make([]string, 9)
		rowChars[y][0] = " " // Empty
		for k := 1; k <= 8; k++ {
			rowChars[y][k] = style.Render(blocks[k-1]) // blocks is 0-indexed (space is 0) but we use 1-8 for blocks
		}
		// rowChars[y][0] = " " (unstyled or styled space)
		// rowChars[y][1..8] = styled blocks

		// blocks := []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
		rowChars[y] = make([]string, len(blocks))
		for k, b := range blocks {
			rowChars[y][k] = style.Render(b)
		}
	}

	// 2. Scale data to fill full width
	// Each data point spans multiple columns to fill the graph
	if len(data) > 0 {
		colsPerPoint := float64(width) / float64(len(data))

		for i, val := range data {
			if val < 0 {
				val = 0
			}

			pct := val / maxVal
			if pct > 1.0 {
				pct = 1.0
			}
			totalSubBlocks := pct * float64(height) * 8.0

			// Calculate column range for this data point
			startCol := int(float64(i) * colsPerPoint)
			endCol := int(float64(i+1) * colsPerPoint)
			if endCol > width {
				endCol = width
			}

			// Draw the bar across all columns for this data point
			for col := startCol; col < endCol; col++ {
				for y := 0; y < height; y++ {
					rowIndex := height - 1 - y
					rowValue := totalSubBlocks - float64(y*8)

					var charIndex int
					if rowValue <= 0 {
						charIndex = 0 // Space
					} else if rowValue >= 8 {
						charIndex = 7 // Full block (█)
					} else {
						charIndex = int(rowValue) // Partial block
					}

					// USE PRE-RENDERED CACHE
					if charIndex > 0 { // Only render if not empty space (optimization)
						rows[rowIndex][col] = rowChars[y][charIndex]
					}
				}
			}
		}
	}

	// 3. Join rows to create the graph
	var graphBuilder strings.Builder
	for i, row := range rows {
		graphBuilder.WriteString(strings.Join(row, ""))
		if i < height-1 {
			graphBuilder.WriteRune('\n')
		}
	}
	graphStr := graphBuilder.String()

	// 4. If stats provided, overlay them on the right side
	if stats != nil {
		graphStr = overlayStatsBox(graphStr, stats, width, height)
	}

	return graphStr
}

// overlayStatsBox renders stats on top of the graph in the top-right area
func overlayStatsBox(graph string, stats *GraphStats, width, height int) string {
	// Create the stats box content - btop style
	valueStyle := lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorLightGray)
	headerStyle := lipgloss.NewStyle().Foreground(ColorNeonPink).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(ColorGray)

	speedMbps := stats.DownloadSpeed * 8
	topMbps := stats.DownloadTop * 8

	// Compact stats box like btop
	statsLines := []string{
		headerStyle.Render("download"),
		fmt.Sprintf("%s %s  %s",
			valueStyle.Render("▼"),
			valueStyle.Render(fmt.Sprintf("%.2f MB/s", stats.DownloadSpeed)),
			dimStyle.Render(fmt.Sprintf("(%.0f Mbps)", speedMbps)),
		),
		fmt.Sprintf("%s %s %s  %s",
			labelStyle.Render("▼"),
			labelStyle.Render("Top:"),
			valueStyle.Render(fmt.Sprintf("%.2f MB/s", stats.DownloadTop)),
			dimStyle.Render(fmt.Sprintf("(%.0f Mbps)", topMbps)),
		),
		fmt.Sprintf("%s %s %s",
			labelStyle.Render("▼"),
			labelStyle.Render("Total:"),
			valueStyle.Render(utils.ConvertBytesToHumanReadable(stats.DownloadTotal)),
		),
	}

	statsBox := lipgloss.JoinVertical(lipgloss.Right, statsLines...)
	statsWidth := lipgloss.Width(statsBox)
	statsHeight := lipgloss.Height(statsBox)

	if statsWidth >= width || statsHeight >= height {
		return graph
	}

	// Overlay by merging graph lines with stats lines on the right
	graphLines := strings.Split(graph, "\n")
	statsBoxLines := strings.Split(statsBox, "\n")

	for i := 0; i < len(statsBoxLines) && i < len(graphLines); i++ {
		graphLineWidth := lipgloss.Width(graphLines[i])
		statsLineWidth := lipgloss.Width(statsBoxLines[i])

		keepWidth := graphLineWidth - statsLineWidth - 1
		if keepWidth < 0 {
			keepWidth = 0
		}

		graphRunes := []rune(graphLines[i])
		if keepWidth < len(graphRunes) {
			graphLines[i] = string(graphRunes[:keepWidth]) + " " + statsBoxLines[i]
		} else {
			padding := keepWidth - len(graphRunes)
			graphLines[i] = graphLines[i] + strings.Repeat(" ", padding) + " " + statsBoxLines[i]
		}
	}

	return strings.Join(graphLines, "\n")
}
