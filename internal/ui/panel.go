package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Panels are lazygit-style boxes: a rounded border with titles set into the
// top edge. A panel with several titles uses them as tabs; the focused
// panel's border is highlighted so it's obvious where keys go.

var (
	focusedBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	activeTabStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
)

// tab is one title in a panel's top edge.
type tab struct {
	label  string
	active bool
}

// panel draws content inside a width×height box. tabs go top-left (a single
// tab is just a title), right goes top-right if there's room. content lines
// are fitted to the inner width and padded to the inner height.
func panel(width, height int, focused bool, tabs []tab, right string, content []string) []string {
	if width < 2 || height < 2 {
		return padLines(nil, height, max(width, 0))
	}
	inner, innerH := width-2, height-2
	border := borderStyle
	if focused {
		border = focusedBorderStyle
	}

	lines := make([]string, 0, height)
	lines = append(lines, panelTop(inner, border, focused, tabs, right))
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		lines = append(lines, border.Render("│")+fit(line, inner)+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return lines
}

// panelTop renders "╭─ Title ─ Other ──── right ─╮" in exactly inner+2 cells,
// dropping the right-hand text and then truncating titles when space runs out.
func panelTop(inner int, border lipgloss.Style, focused bool, tabs []tab, right string) string {
	var titles []string
	for _, t := range tabs {
		style := dimStyle
		if t.active {
			style = boldStyle
			if focused {
				style = activeTabStyle
			}
		}
		titles = append(titles, style.Render(t.label))
	}
	left := ""
	if len(titles) > 0 {
		left = " " + strings.Join(titles, border.Render(" ─ ")) + " "
	}
	if right != "" {
		right = " " + dimStyle.Render(right) + " "
	}

	// Leave at least one "─" on each side of the titles.
	room := inner - 2
	if ansi.StringWidth(left)+ansi.StringWidth(right) > room {
		right = ""
	}
	if ansi.StringWidth(left) > room {
		left = ansi.Truncate(left, max(room, 0), "…")
	}
	fill := inner - 2 - ansi.StringWidth(left) - ansi.StringWidth(right)
	if inner < 2 {
		return border.Render("╭" + strings.Repeat("─", inner) + "╮")
	}
	return border.Render("╭─") + left + border.Render(strings.Repeat("─", max(fill, 0))) + right + border.Render("─╮")
}
