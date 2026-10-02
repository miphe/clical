package ui

import (
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// Styles mostly use the terminal's own 16-color palette so they follow the
// user's terminal theme, light or dark. Real themes arrive in M9.
var (
	// The first calendars use the terminal's own colors (so they follow its
	// theme): six normal, then six bright. Later ones get generated hues.
	terminalPalette = []color.Color{
		lipgloss.Color("4"), lipgloss.Color("2"), lipgloss.Color("5"),
		lipgloss.Color("3"), lipgloss.Color("6"), lipgloss.Color("1"),
		lipgloss.Color("12"), lipgloss.Color("10"), lipgloss.Color("13"),
		lipgloss.Color("11"), lipgloss.Color("14"), lipgloss.Color("9"),
	}

	boldStyle     = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	todayStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	nowStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	selectedStyle = lipgloss.NewStyle().Reverse(true)
	// The selection stays visible, but quieter, while the sidebar has focus.
	inactiveSelStyle = lipgloss.NewStyle().Underline(true)
	borderStyle      = lipgloss.NewStyle().Faint(true)
)

// calendarColor maps a calendar's permanent color slot (1-based) to a color.
// Past the terminal palette, hues are spread by the golden angle so
// neighbouring slots stay easy to tell apart, at a lightness readable on both
// light and dark backgrounds.
func calendarColor(slot int) color.Color {
	if slot < 1 {
		slot = 1
	}
	if slot <= len(terminalPalette) {
		return terminalPalette[slot-1]
	}
	n := slot - len(terminalPalette)
	hue := math.Mod(float64(n)*137.508, 360)
	return lipgloss.Color(colorful.Hcl(hue, 0.55, 0.62).Clamped().Hex())
}
