package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/miphe/clical/internal/domain"
)

// renderHeat draws the heatmap for a width×height area inside its panel.
func (m Model) renderHeat(width, height int) []string {
	note := func(s string) []string {
		lines := strings.Split(lipgloss.NewStyle().Width(width).Render(s), "\n")
		for i := range lines {
			lines[i] = mutedStyle.Render(fit(lines[i], width))
		}
		return padLines(lines, height, width)
	}
	switch {
	case m.writes == nil:
		return note("The heatmap needs the notes folder, which isn't available.")
	case width-heatLabelW < 4*heatCellW:
		return note("Too narrow for the heatmap. Widen the window, or press 1 for the agenda.")
	case height < heatRows:
		return note("Too short for the heatmap. Make the window taller, or press 1 for the agenda.")
	}

	h := m.heat
	_, hi := h.bounds(m.today())
	cellsW := width - heatLabelW
	cols := heatColumns(h.viewLeft(cellsW, hi), hi, cellsW)
	selLo, selHi := h.span(m.weekStart)

	lines := make([]string, 0, height)
	for r := 0; r < heatRows; r++ {
		var b strings.Builder
		b.WriteString(mutedStyle.Render(weekdayLetter(time.Weekday(r)) + " "))
		for _, c := range cols {
			if c.gap {
				b.WriteString(strings.Repeat(" ", heatGapW))
			}
			if d, ok := c.day(r); ok {
				b.WriteString(m.heatCell(d, hi, selLo, selHi))
			} else {
				b.WriteString(heatFiller)
			}
		}
		lines = append(lines, b.String())
	}
	for i := range lines {
		lines[i] = fit(lines[i], width)
	}
	return padLines(lines, height, width)
}

// heatFiller fills the cells of a month's block that aren't a day, so the
// block is square.
var heatFiller = heatFillStyle.Render(heatNone + " ")

// heatCell is one day, two columns wide. A selected day keeps its color and
// shows a dot inside its square, in bold; the days after the last one are
// blank.
func (m Model) heatCell(d, hi, selLo, selHi domain.CivilDate) string {
	if hi.Before(d) {
		return strings.Repeat(" ", heatCellW)
	}
	level := heatLevel(len(m.heat.days[d]))
	glyph := heatDone
	if level == 0 {
		glyph = heatNone
	}
	if !d.Before(selLo) && !selHi.Before(d) {
		return heatStyles[level].Bold(true).Render(heatSel + " ")
	}
	return heatStyles[level].Render(glyph + " ")
}

// heatLegend says what the colors mean, for the heatmap panel's top edge.
func heatLegend() string {
	key := func(level int, glyph, label string) string {
		return heatStyles[level].Render(glyph) + mutedStyle.Render(" "+label)
	}
	return key(0, heatNone, "none") + mutedStyle.Render("  ") + key(1, heatDone, "1–3") +
		mutedStyle.Render("  ") + key(2, heatDone, "4–6") + mutedStyle.Render("  ") + key(3, heatDone, "7+")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// heatTitle names the selection: "Week 41 · 5–11 Oct 2026", "October 2026",
// or the day or days.
func (m Model) heatTitle(lo, hi domain.CivilDate) string {
	switch m.heat.unit {
	case heatMonth:
		if sameMonth(lo, hi) {
			return lo.In(m.loc).Format("January 2006")
		}
		return lo.In(m.loc).Format("January 2006") + " – " + hi.In(m.loc).Format("January 2006")
	case heatWeek:
		label := fmt.Sprintf("Week %d", m.isoWeek(lo))
		if lo.AddDays(6) != hi {
			label = fmt.Sprintf("Weeks %d–%d", m.isoWeek(lo), m.isoWeek(hi.AddDays(-6)))
		}
		return label + " · " + m.dayRange(lo, hi)
	}
	return m.dayRange(lo, hi)
}

// isoWeek is the ISO week number of the week that starts on first, whichever
// day weeks start: ISO numbers a week by its Thursday.
func (m Model) isoWeek(first domain.CivilDate) int {
	thursday := first.AddDays((int(time.Thursday) - int(first.Weekday()) + 7) % 7)
	_, week := thursday.In(m.loc).ISOWeek()
	return week
}

// dayRange writes days as "Mon 5 Oct 2026", "5–9 Oct 2026" or
// "28 Sep – 3 Oct 2026".
func (m Model) dayRange(lo, hi domain.CivilDate) string {
	l, h := lo.In(m.loc), hi.In(m.loc)
	switch {
	case lo == hi:
		return l.Format("Mon 2 Jan 2006")
	case sameMonth(lo, hi):
		return l.Format("2") + "–" + h.Format("2 Jan 2006")
	case lo.Year == hi.Year:
		return l.Format("2 Jan") + " – " + h.Format("2 Jan 2006")
	}
	return l.Format("2 Jan 2006") + " – " + h.Format("2 Jan 2006")
}
