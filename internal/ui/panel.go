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
	legend  string // top-right, already styled; used instead of right
	bottom  string // bottom-right, e.g. a list position "3 of 15"
	flash   bool   // the border is lit for a moment
	scroll  scrollbar
}

// scrollbar is the part of a panel's content that scrolls, for the bar in
// its right border: the content rows it takes, from top, and which of its
// lines they show. Every panel whose content can be more than it shows sets
// it; the bar appears only while something is out of view. The zero value
// is no bar.
type scrollbar struct {
	top, rows    int // content rows [top, top+rows) scroll
	first, total int // they show lines [first, first+rows) of total
}

// thumb is where the bar's thumb is in rows rows: its first row and its
// length, in proportion to what is shown. It is at the top only when nothing
// is hidden above and at the bottom only when nothing is hidden below, so a
// glance tells whether there is more either way. ok is false when everything
// is shown.
func (s scrollbar) thumb() (at, size int, ok bool) {
	total := max(s.total, s.first+s.rows) // a list kept above its end, with room under it
	if s.rows < 2 || total <= s.rows {
		return 0, 0, false
	}
	size = min(max((s.rows*s.rows+total/2)/total, 1), s.rows-1)
	travel := s.rows - size
	at = (s.first*travel + (total-s.rows)/2) / (total - s.rows)
	if s.first > 0 {
		at = max(at, 1)
	}
	if s.first+s.rows < total {
		at = min(at, travel-1)
	}
	return min(max(at, 0), travel), size, true
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
	lines = append(lines, panelTop(inner, border, focused, tabs, right, f.legend))
	at, size, bar := f.scroll.thumb()
	at += f.scroll.top
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		edge := "│"
		if bar && i >= at && i < at+size {
			edge = "┃"
		}
		lines = append(lines, border.Render("│")+fit(line, inner)+border.Render(edge))
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
func panelTop(inner int, border lipgloss.Style, focused bool, tabs []tab, right, legend string) string {
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
	if legend != "" {
		right = " " + legend + " "
	} else if right != "" {
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
