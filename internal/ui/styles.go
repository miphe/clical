package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Styles use the terminal's own 16-color palette so they follow the user's
// terminal theme, light or dark. Real themes arrive in M9.
var (
	calendarPalette = []color.Color{
		lipgloss.Color("4"), lipgloss.Color("2"), lipgloss.Color("5"),
		lipgloss.Color("3"), lipgloss.Color("6"), lipgloss.Color("1"),
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
	detailBoxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func calendarColor(i int) color.Color {
	return calendarPalette[i%len(calendarPalette)]
}
