package tui

import (
	"fmt"
	"strings"
	"time"

	"surge/internal/utils"

	"github.com/charmbracelet/lipgloss"
)

// Define the Layout Ratios
const (
	ListWidthRatio = 0.6 // List takes 60% width
)

func (m RootModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// === Handle Modal States First ===
	// These overlays sit on top of the dashboard or replace it

	if m.state == InputState {
		labelStyle := lipgloss.NewStyle().Width(10).Foreground(ColorLightGray)
		// Centered popup - compact layout
		hintStyle := lipgloss.NewStyle().MarginLeft(1).Foreground(ColorLightGray) // Secondary
		if m.focusedInput == 1 {
			hintStyle = lipgloss.NewStyle().MarginLeft(1).Foreground(ColorNeonPink) // Highlighted
		}
		pathLine := lipgloss.JoinHorizontal(lipgloss.Left,
			labelStyle.Render("Path:"),
			m.inputs[1].View(),
			hintStyle.Render("[Tab] Browse"),
		)

		// Content layout - removing TitleStyle Render and adding spacers
		content := lipgloss.JoinVertical(lipgloss.Left,
			"", // Top spacer
			lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("URL:"), m.inputs[0].View()),
			"", // Spacer
			pathLine,
			"", // Spacer
			lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render("Filename:"), m.inputs[2].View()),
			"", // Bottom spacer
			"",
			// Render dynamic help
			m.help.View(InputKeys),
		)

		// Apply padding to the content before boxing it
		paddedContent := lipgloss.NewStyle().Padding(0, 2).Render(content)

		box := renderBtopBox("Add Download", paddedContent, 80, 11, ColorNeonPink, false)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	if m.state == FilePickerState {
		pickerContent := lipgloss.JoinVertical(lipgloss.Left,
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render(m.filepicker.CurrentDirectory),
			"",
			m.filepicker.View(),
			"",
			m.help.View(FilePickerKeys),
		)

		paddedContent := lipgloss.NewStyle().Padding(0, 2).Render(pickerContent)

		box := renderBtopBox("Select Directory", paddedContent, 80, 20, ColorNeonPink, false)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}

	if m.state == DuplicateWarningState {
		warningContent := lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonPink).Bold(true).Render("⚠ DUPLICATE DETECTED"),
			"",
			lipgloss.NewStyle().Foreground(ColorNeonPurple).Bold(true).Render(truncateString(m.duplicateInfo, 50)),
			"",
			lipgloss.NewStyle().Foreground(ColorLightGray).Render("[C] Continue  [F] Focus Existing  [X] Cancel"),
		)

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(ColorNeonPink).
				Padding(1, 3).
				Render(warningContent),
		)
	}

	// === MAIN DASHBOARD LAYOUT ===

	availableHeight := m.height - 2 // Margin
	availableWidth := m.width - 4   // Margin

	// Column Widths
	leftWidth := int(float64(availableWidth) * ListWidthRatio)
	rightWidth := availableWidth - leftWidth - 2 // -2 for spacing

	// --- LEFT COLUMN HEIGHTS ---
	headerHeight := 11
	listHeight := availableHeight - headerHeight
	if listHeight < 10 {
		listHeight = 10
	}

	// --- RIGHT COLUMN HEIGHTS ---
	graphHeight := availableHeight / 3
	if graphHeight < 9 {
		graphHeight = 9
	}
	detailHeight := availableHeight - graphHeight
	if detailHeight < 10 {
		detailHeight = 10
	}

	// --- SECTION 1: HEADER & LOGO (Top Left) + LOG BOX (Top Right) ---
	logoText := `
   _______  ___________ ____ 
  / ___/ / / / ___/ __ '/ _ \
 (__  ) /_/ / /  / /_/ /  __/
/____/\__,_/_/   \__, /\___/ 
                /____/       `

	// Calculate stats for tab bar
	active, queued, downloaded := m.CalculateStats()

	// Logo takes ~45% of header width
	logoWidth := int(float64(leftWidth) * 0.45)
	logWidth := leftWidth - logoWidth - 2 // Rest for log box

	// Render logo centered in its box
	logoBox := lipgloss.Place(logoWidth, headerHeight, lipgloss.Center, lipgloss.Center,
		LogoStyle.Render(logoText))

	// Render log viewport
	m.logViewport.Width = logWidth - 4      // Account for borders
	m.logViewport.Height = headerHeight - 4 // Account for borders and title
	logContent := m.logViewport.View()

	// Use different border color when focused
	logBorderColor := ColorNeonCyan
	if m.logFocused {
		logBorderColor = ColorNeonPink
	}
	logBox := renderBtopBox("Activity Log", logContent, logWidth, headerHeight, logBorderColor, false)

	// Combine logo and log box horizontally
	headerBox := lipgloss.JoinHorizontal(lipgloss.Top, logoBox, logBox)

	// --- SECTION 2: SPEED GRAPH (Top Right) ---
	// Use 60 data points for 30 seconds of history (2 samples per second / 0.5s interval)
	const graphHistoryPoints = 60

	// Stats box takes ~20 chars width, graph takes the rest
	statsBoxWidth := 20
	graphContentWidth := rightWidth - statsBoxWidth - 6 // borders + spacing
	if graphContentWidth < 20 {
		graphContentWidth = 20
	}

	// Get the last 60 data points for the graph
	var graphData []float64
	if len(m.SpeedHistory) > graphHistoryPoints {
		graphData = m.SpeedHistory[len(m.SpeedHistory)-graphHistoryPoints:]
	} else {
		graphData = m.SpeedHistory
	}

	// Determine Max Speed for scaling
	maxSpeed := 1.0 // Prevent divide by zero
	topSpeed := 0.0
	for _, v := range graphData {
		if v > maxSpeed {
			maxSpeed = v
		}
		if v > topSpeed {
			topSpeed = v
		}
	}
	// Add headroom
	maxSpeed = maxSpeed * 1.1
	if maxSpeed >= 5 {
		maxSpeed = float64(int((maxSpeed+4.99)/5) * 5)
	} else if maxSpeed >= 1 {
		maxSpeed = float64(int(maxSpeed + 0.99))
	}

	// Calculate Available Height for the Graph
	// graphHeight - Borders (2) - title area (1)
	graphContentHeight := graphHeight - 3
	if graphContentHeight < 1 {
		graphContentHeight = 1
	}

	// Get current speed and calculate total downloaded
	currentSpeed := 0.0
	if len(m.SpeedHistory) > 0 {
		currentSpeed = m.SpeedHistory[len(m.SpeedHistory)-1]
	}

	// Calculate total downloaded across all downloads
	var totalDownloaded int64
	for _, d := range m.downloads {
		totalDownloaded += d.Downloaded
	}

	// Render the Graph (no stats overlay - stats go in separate box)
	axisWidth := 6
	graphVisual := renderMultiLineGraph(graphData, graphContentWidth-axisWidth-1, graphContentHeight, maxSpeed, ColorNeonPink, nil)

	// Create Y-axis (right side of graph)
	axisStyle := lipgloss.NewStyle().Width(axisWidth).Foreground(ColorGray).Align(lipgloss.Right)
	labelTop := axisStyle.Render(fmt.Sprintf("%.0f", maxSpeed))
	labelMid := axisStyle.Render(fmt.Sprintf("%.1f", maxSpeed/2))
	labelBot := axisStyle.Render("0")

	var axisColumn string
	if graphContentHeight >= 5 {
		spacesTotal := graphContentHeight - 3
		spaceTop := spacesTotal / 2
		spaceBot := spacesTotal - spaceTop
		axisColumn = lipgloss.JoinVertical(lipgloss.Right,
			labelTop,
			strings.Repeat("\n", spaceTop),
			labelMid,
			strings.Repeat("\n", spaceBot),
			labelBot,
		)
	} else {
		spaces := graphContentHeight - 2
		if spaces < 0 {
			spaces = 0
		}
		axisColumn = lipgloss.JoinVertical(lipgloss.Right,
			labelTop,
			strings.Repeat("\n", spaces),
			labelBot,
		)
	}

	// Combine graph + axis (axis on right)
	graphWithAxis := lipgloss.JoinHorizontal(lipgloss.Top,
		graphVisual,
		axisColumn,
	)

	// Create stats box content
	speedMbps := currentSpeed * 8
	topMbps := topSpeed * 8

	valueStyle := lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true)
	labelStyleStats := lipgloss.NewStyle().Foreground(ColorLightGray)
	dimStyle := lipgloss.NewStyle().Foreground(ColorGray)

	statsContent := lipgloss.JoinVertical(lipgloss.Left,
		fmt.Sprintf("%s %s", valueStyle.Render("▼"), valueStyle.Render(fmt.Sprintf("%.2f MB/s", currentSpeed))),
		dimStyle.Render(fmt.Sprintf("  (%.0f Mbps)", speedMbps)),
		"",
		fmt.Sprintf("%s %s", labelStyleStats.Render("▼ Top:"), valueStyle.Render(fmt.Sprintf("%.2f MB/s", topSpeed))),
		dimStyle.Render(fmt.Sprintf("  (%.0f Mbps)", topMbps)),
		"",
		fmt.Sprintf("%s %s", labelStyleStats.Render("▼ Total:"), valueStyle.Render(formatBytes(totalDownloaded))),
	)

	// Render graph and stats boxes side by side
	graphInnerBox := renderBtopBox("Network Activity", graphWithAxis, rightWidth-statsBoxWidth-1, graphHeight, ColorNeonCyan, false)
	statsInnerBox := renderBtopBox("Stats", statsContent, statsBoxWidth, graphHeight, ColorNeonCyan, false)

	graphBox := lipgloss.JoinHorizontal(lipgloss.Top, graphInnerBox, statsInnerBox)

	// --- SECTION 3: DOWNLOAD LIST (Bottom Left) ---
	// Tab Bar
	tabBar := renderTabs(m.activeTab, active, queued, downloaded)

	// Render the bubbles list or centered empty message
	var listContent string
	if len(m.list.Items()) == 0 {
		// FIX: Reduced width (leftWidth-8) to account for padding (4) and borders (2) + safety
		// preventing the "floating bits" wrap-around artifact.
		listContentHeight := listHeight - 6
		listContent = lipgloss.Place(leftWidth-8, listContentHeight, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Render("No downloads"))
	} else {
		// ensure list fills the height
		m.list.SetHeight(listHeight - 4) // adjust for padding/tabs
		listContent = m.list.View()
	}

	listInner := lipgloss.NewStyle().Padding(1, 2).Render(lipgloss.JoinVertical(lipgloss.Left,
		tabBar,
		listContent,
	))
	// Determine border color for downloads box based on focus
	downloadsBorderColor := ColorNeonPink
	if m.logFocused {
		downloadsBorderColor = ColorGray
	}
	listBox := renderBtopBox("Downloads", listInner, leftWidth, listHeight, downloadsBorderColor, true)

	// --- SECTION 4: DETAILS PANE (Bottom Right) ---
	var detailContent string
	if d := m.GetSelectedDownload(); d != nil {
		detailContent = renderFocusedDetails(d, rightWidth-4)
	} else {
		detailContent = lipgloss.Place(rightWidth-4, detailHeight-4, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Render("No Download Selected"))
	}

	detailBox := renderBtopBox("File Details", detailContent, rightWidth, detailHeight, ColorGray, true)

	// --- ASSEMBLY ---

	// Left Column
	leftColumn := lipgloss.JoinVertical(lipgloss.Left, headerBox, listBox)

	// Right Column
	rightColumn := lipgloss.JoinVertical(lipgloss.Left, graphBox, detailBox)

	// Body
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftColumn, rightColumn)

	// Footer - just keybindings
	footer := lipgloss.NewStyle().Foreground(ColorLightGray).Padding(0, 1).Render(" [Q/W/E] Tabs  [A] Add  [P] Pause  [X] Delete  [L] Log  [/] Filter  [Ctrl+Q] Quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		body,
		footer,
	)
}

// Helper to render the detailed info pane
func renderFocusedDetails(d *DownloadModel, w int) string {
	pct := 0.0
	if d.Total > 0 {
		pct = float64(d.Downloaded) / float64(d.Total)
	}

	// Progress bar with margins
	progressWidth := w - 12
	if progressWidth < 20 {
		progressWidth = 20
	}
	d.progress.Width = progressWidth
	progView := d.progress.ViewAs(pct)
	// pctStr was previously used for explicit percentage display

	// Consistent content width for centering
	contentWidth := w - 6

	// Section divider
	divider := lipgloss.NewStyle().
		Foreground(ColorGray).
		Render(strings.Repeat("─", contentWidth))

	// File info section - compact layout
	fileInfo := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Filename:"), StatsValueStyle.Render(truncateString(d.Filename, contentWidth-14))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Status:"), StatsValueStyle.Render(getDownloadStatus(d))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Size:"), StatsValueStyle.Render(fmt.Sprintf("%s / %s", utils.ConvertBytesToHumanReadable(d.Downloaded), utils.ConvertBytesToHumanReadable(d.Total)))),
	)

	// Progress section - compact
	progressLabel := lipgloss.NewStyle().
		Foreground(ColorNeonCyan).
		Bold(true).
		Render("Progress")
	progressSection := lipgloss.JoinVertical(lipgloss.Left,
		progressLabel,
		"",
		lipgloss.NewStyle().MarginLeft(1).Render(progView),
	)

	// Stats section
	statsSection := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Speed:"), StatsValueStyle.Render(fmt.Sprintf("%.2f MB/s", d.Speed/Megabyte))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Conns:"), StatsValueStyle.Render(fmt.Sprintf("%d", d.Connections))),
		lipgloss.JoinHorizontal(lipgloss.Left, StatsLabelStyle.Render("Elapsed:"), StatsValueStyle.Render(d.Elapsed.Round(time.Second).String())),
	)

	// URL section
	urlSection := lipgloss.JoinHorizontal(lipgloss.Left,
		StatsLabelStyle.Render("URL:"),
		lipgloss.NewStyle().Foreground(ColorLightGray).Render(truncateString(d.URL, contentWidth-14)),
	)

	// Combine all sections - dense layout with dividers
	content := lipgloss.JoinVertical(lipgloss.Left,
		"",
		fileInfo,
		divider,
		"",
		progressSection,
		divider,
		"",
		statsSection,
		divider,
		"",
		urlSection,
	)

	// Wrap in a container with reduced padding
	return lipgloss.NewStyle().
		Padding(0, 2).
		Render(content)
}

func getDownloadStatus(d *DownloadModel) string {
	style := lipgloss.NewStyle()

	switch {
	case d.err != nil:
		return style.Foreground(ColorStateError).Render("✖ Error")
	case d.done:
		return style.Foreground(ColorStateDone).Render("✔ Completed")
	case d.paused:
		return style.Foreground(ColorStatePaused).Render("⏸ Paused")
	case d.Speed == 0 && d.Downloaded == 0:
		return style.Foreground(ColorStatePaused).Render("o Queued")
	default:
		return style.Foreground(ColorStateDownloading).Render("⬇ Downloading")
	}
}

func (m RootModel) calcTotalSpeed() float64 {
	total := 0.0
	for _, d := range m.downloads {
		// Skip completed downloads
		if d.done {
			continue
		}
		total += d.Speed
	}
	return total / Megabyte
}

func (m RootModel) CalculateStats() (active, queued, downloaded int) {
	for _, d := range m.downloads {
		if d.done {
			downloaded++
		} else if d.Speed > 0 {
			active++
		} else {
			queued++
		}
	}
	return
}

func truncateString(s string, i int) string {
	runes := []rune(s)
	if len(runes) > i {
		return string(runes[:i]) + "..."
	}
	return s
}

func renderTabs(activeTab, activeCount, queuedCount, doneCount int) string {
	tabs := []struct {
		Label string
		Count int
	}{
		{"Queued", queuedCount},
		{"Active", activeCount},
		{"Done", doneCount},
	}
	var rendered []string
	for i, t := range tabs {
		var style lipgloss.Style
		if i == activeTab {
			style = ActiveTabStyle
		} else {
			style = TabStyle
		}
		label := fmt.Sprintf("%s (%d)", t.Label, t.Count)
		rendered = append(rendered, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// renderBtopBox creates a btop-style box with title embedded in the top border
// titleRight: if true, title appears on the right side; if false, title appears on the left
// Example (left):  ╭─ TITLE ─────────────────────────────────╮
// Example (right): ╭─────────────────────────────────── TITLE ─╮
func renderBtopBox(title string, content string, width, height int, borderColor lipgloss.Color, titleRight bool) string {
	// Border characters
	const (
		topLeft     = "╭"
		topRight    = "╮"
		bottomLeft  = "╰"
		bottomRight = "╯"
		horizontal  = "─"
		vertical    = "│"
	)

	innerWidth := width - 2 // Account for left and right borders
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Build top border with embedded title
	titleText := fmt.Sprintf(" %s ", title)
	titleLen := len(titleText)
	remainingWidth := innerWidth - titleLen - 1 // -1 for the dash after topLeft
	if remainingWidth < 0 {
		remainingWidth = 0
	}

	var topBorder string
	if titleRight {
		// Title on the right: ╭─────────────────────────────────── TITLE ─╮
		topBorder = lipgloss.NewStyle().Foreground(borderColor).Render(topLeft+strings.Repeat(horizontal, remainingWidth)) +
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true).Render(titleText) +
			lipgloss.NewStyle().Foreground(borderColor).Render(horizontal+topRight)
	} else {
		// Title on the left: ╭─ TITLE ─────────────────────────────────╮
		topBorder = lipgloss.NewStyle().Foreground(borderColor).Render(topLeft+horizontal) +
			lipgloss.NewStyle().Foreground(ColorNeonCyan).Bold(true).Render(titleText) +
			lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat(horizontal, remainingWidth)) +
			lipgloss.NewStyle().Foreground(borderColor).Render(topRight)
	}

	// Build bottom border: ╰───────────────────╯
	bottomBorder := lipgloss.NewStyle().Foreground(borderColor).Render(
		bottomLeft + strings.Repeat(horizontal, innerWidth) + bottomRight,
	)

	// Style for vertical borders
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Wrap content lines with vertical borders
	contentLines := strings.Split(content, "\n")
	innerHeight := height - 2 // Account for top and bottom borders

	var wrappedLines []string
	for i := 0; i < innerHeight; i++ {
		var line string
		if i < len(contentLines) {
			line = contentLines[i]
		} else {
			line = ""
		}
		// Pad or truncate line to fit innerWidth
		lineWidth := lipgloss.Width(line)
		if lineWidth < innerWidth {
			line = line + strings.Repeat(" ", innerWidth-lineWidth)
		} else if lineWidth > innerWidth {
			// Truncate (simplified - just take first innerWidth chars)
			runes := []rune(line)
			if len(runes) > innerWidth {
				line = string(runes[:innerWidth])
			}
		}
		wrappedLines = append(wrappedLines, borderStyle.Render(vertical)+line+borderStyle.Render(vertical))
	}

	// Combine all parts
	return lipgloss.JoinVertical(lipgloss.Left,
		topBorder,
		strings.Join(wrappedLines, "\n"),
		bottomBorder,
	)
}
