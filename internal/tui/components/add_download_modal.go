package components

import (
	"github.com/surge-downloader/surge/internal/tui/colors"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// AddDownloadModal renders input-driven download forms (add download / extension prompt).
type AddDownloadModal struct {
	Title           string
	Inputs          []textinput.Model
	Labels          []string
	FocusedInput    int
	ShowURL         bool
	URL             string
	BrowseHintIndex int
	Help            help.Model
	HelpKeys        help.KeyMap
	BorderColor     lipgloss.TerminalColor
	Width           int
	Height          int
}

// View renders the inner content (without border box).
func (m AddDownloadModal) View() string {
	labelStyle := lipgloss.NewStyle().Width(10).Foreground(colors.LightGray)
	hintBase := lipgloss.NewStyle().MarginLeft(1).Foreground(colors.LightGray)
	content := []string{""}

	if m.ShowURL && m.URL != "" {
		content = append(content,
			lipgloss.NewStyle().Foreground(colors.LightGray).Render("URL: "+m.URL),
			"",
		)
	}

	for i := 0; i < len(m.Inputs) && i < len(m.Labels); i++ {
		row := lipgloss.JoinHorizontal(lipgloss.Left, labelStyle.Render(m.Labels[i]), m.Inputs[i].View())
		if m.BrowseHintIndex == i {
			hintStyle := hintBase
			if m.FocusedInput == i {
				hintStyle = hintStyle.Foreground(colors.NeonPink)
			}
			row = lipgloss.JoinHorizontal(lipgloss.Left, row, hintStyle.Render("[Tab] Browse"))
		}
		content = append(content, row, "")
	}

	content = append(content, m.Help.View(m.HelpKeys))
	return lipgloss.NewStyle().Padding(0, 2).Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

// RenderWithBtopBox renders the modal with btop-style border.
func (m AddDownloadModal) RenderWithBtopBox(
	renderBox func(leftTitle, rightTitle, content string, width, height int, borderColor lipgloss.TerminalColor) string,
	titleStyle lipgloss.Style,
) string {
	return renderBox(titleStyle.Render(" "+m.Title+" "), "", m.View(), m.Width, m.Height, m.BorderColor)
}

