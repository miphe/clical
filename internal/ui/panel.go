package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Panels are lazygit-style boxes: a rounded border with titles set into the
// top edge. A panel with several titles uses them as tabs; the focused
// panel's border is highlighted so it's obvious where keys go.

// tab is one title in a panel's top edge.
type tab struct {
	label  string
	active bool
}

// frame is what a panel shows around its content.
type frame struct {
	focused bool
	tabs    []tab  // top-left; a single tab is just a title
	right   string // top-right, if there's room
	bottom  string // bottom-right, e.g. a list position "3 of 15"
	flash   bool   // the border is lit for a moment
}

// panel draws content inside a width×height box. content lines are fitted to
// the inner width and padded to the inner height.
func panel(width, height int, f frame, content []string) []string {
	focused, tabs, right := f.focused, f.tabs, f.right
	if width < 2 || height < 2 {
		return padLines(nil, height, max(width, 0))
	}
	inner, innerH := width-2, height-2
	border := borderStyle
	if focused {
		border = focusedBorderStyle
	}
	if f.flash {
		border = flashBorderStyle
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
	lines = append(lines, panelBottom(inner, border, f.bottom))
	return lines
}

func panelBottom(inner int, border lipgloss.Style, label string) string {
	if label != "" {
		label = " " + mutedStyle.Render(label) + " "
	}
	if label == "" || ansi.StringWidth(label)+2 > inner {
		return border.Render("╰" + strings.Repeat("─", max(inner, 0)) + "╯")
	}
	fill := inner - 1 - ansi.StringWidth(label)
	return border.Render("╰"+strings.Repeat("─", fill)) + label + border.Render("─╯")
}

// panelTop renders "╭─ Title ─ Other ──── right ─╮" in exactly inner+2 cells,
// dropping the right-hand text and then truncating titles when space runs out.
func panelTop(inner int, border lipgloss.Style, focused bool, tabs []tab, right string) string {
	var titles []string
	for _, t := range tabs {
		style := mutedStyle
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
		right = " " + mutedStyle.Render(right) + " "
	}

	// Leave at least one "─" on each side of the titles, and between the
	// titles and the right-hand text.
	room := inner - 2
	if ansi.StringWidth(left)+ansi.StringWidth(right)+1 > room {
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
