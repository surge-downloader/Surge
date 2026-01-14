package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ApplyGradient applies a vertical gradient to a multi-line string
func ApplyGradient(text string, startColor, endColor lipgloss.Color) string {
	lines := strings.Split(text, "\n")
	height := len(lines)
	if height == 0 {
		return text
	}

	startRGB, _ := hexToRGB(string(startColor))
	endRGB, _ := hexToRGB(string(endColor))

	var coloredLines []string
	for i, line := range lines {
		// Calculate interpolation factor t [0, 1]
		// If there is only one line, t will be 0 (startColor)
		t := 0.0
		if height > 1 {
			t = float64(i) / float64(height-1)
		}

		// Interpolate RGB values
		r := uint8(math.Round(lerp(float64(startRGB.r), float64(endRGB.r), t)))
		g := uint8(math.Round(lerp(float64(startRGB.g), float64(endRGB.g), t)))
		b := uint8(math.Round(lerp(float64(startRGB.b), float64(endRGB.b), t)))

		// Create color string
		hexColor := fmt.Sprintf("#%02x%02x%02x", r, g, b)
		color := lipgloss.Color(hexColor)

		// Apply style to the line
		// Preserving Bold(true) as in the original LogoStyle
		coloredLine := lipgloss.NewStyle().Foreground(color).Bold(true).Render(line)
		coloredLines = append(coloredLines, coloredLine)
	}

	return strings.Join(coloredLines, "\n")
}

type rgb struct {
	r, g, b uint8
}

func hexToRGB(hex string) (rgb, error) {
	// Remove leading hash
	hex = strings.TrimPrefix(hex, "#")

	// Handle short hex (e.g., "FFF")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}

	if len(hex) != 6 {
		return rgb{}, fmt.Errorf("invalid hex color: %s", hex)
	}

	val, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return rgb{}, err
	}

	return rgb{
		r: uint8(val >> 16),
		g: uint8((val >> 8) & 0xFF),
		b: uint8(val & 0xFF),
	}, nil
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}
