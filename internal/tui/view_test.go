package tui

import (
	"regexp"
	"strings"
	"testing"
)

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestView_DashboardFitsViewportWithoutTopCutoff(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, false)

	cases := []struct {
		width  int
		height int
	}{
		{120, 35},
		{100, 30},
		{80, 24},
	}

	for _, tc := range cases {
		m.width = tc.width
		m.height = tc.height

		view := m.View()
		if strings.HasPrefix(view, "\n") {
			t.Fatalf("view starts with a blank line at %dx%d", tc.width, tc.height)
		}

		plain := ansiEscapeRE.ReplaceAllString(view, "")
		trimmed := strings.TrimRight(plain, "\n")
		lines := strings.Split(trimmed, "\n")

		if len(lines) > tc.height {
			t.Fatalf("view exceeds viewport height at %dx%d: got %d lines", tc.width, tc.height, len(lines))
		}

		if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
			t.Fatalf("top line is empty at %dx%d (possible top cutoff)", tc.width, tc.height)
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			t.Fatalf("bottom line is empty at %dx%d (footer likely clipped)", tc.width, tc.height)
		}
	}
}

func TestView_SettingsTinyTerminalDoesNotPanic(t *testing.T) {
	m := InitialRootModel(1701, "test-version", nil, false)
	m.state = SettingsState
	m.width = 20
	m.height = 8

	view := m.View()
	if strings.TrimSpace(ansiEscapeRE.ReplaceAllString(view, "")) == "" {
		t.Fatal("expected non-empty settings view for tiny terminal")
	}
}
