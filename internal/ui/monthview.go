package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

const (
	minMonthColumn = 4
	weekNumWidth   = 2 // ISO week numbers, 1–53
)

// renderMonth draws the month as a grid of week rows for a width×height area
// inside the main panel. A narrow column of ISO week numbers runs down the
// left (Monday-first weeks only). Each cell shows its date, then as many
// events as fit, then "+N more".
func (m Model) renderMonth(width, height int) []string {
	numW := 0
	if m.weekStart == time.Monday {
		numW = weekNumWidth + 1 // plus its separator
	}
	cols := columnWidths(width-numW, 7)
	if cols[0] < minMonthColumn {
		msg := lipgloss.NewStyle().Width(width).Render("Too narrow for the month view. Widen the window, or press 1 for the agenda.")
		lines := strings.Split(msg, "\n")
		for i := range lines {
			lines[i] = mutedStyle.Render(fit(lines[i], width))
		}
		return padLines(lines, height, width)
	}
	if !m.month.loaded {
		line := mutedStyle.Render("Loading…")
		if m.loadErr != nil {
			line = errorStyle.Render("Can't read the local cache: " + m.loadErr.Error())
		}
		return padLines([]string{fit(line, width)}, height, width)
	}

	sep := borderStyle.Render("│")
	numCell := func(s string) string {
		if numW == 0 {
			return ""
		}
		return mutedStyle.Render(fit(s, weekNumWidth)) + sep
	}

	// Weekday header and rule.
	var names, rules []string
	for d, w := range cols {
		names = append(names, mutedStyle.Render(fit(m.month.start.AddDays(d).In(m.loc).Format("Mon"), w)))
		rules = append(rules, strings.Repeat("─", w))
	}
	rule := strings.Join(rules, "┼")
	if numW > 0 {
		rule = strings.Repeat("─", weekNumWidth) + "┼" + rule
	}
	rule = borderStyle.Render(rule)
	lines := []string{numCell("") + strings.Join(names, sep), rule}

	// Week rows, separated by a rule when there's room for one.
	bodyH := height - len(lines)
	weeks := m.month.weeks
	ruled := (bodyH-(weeks-1))/weeks >= 3
	avail := bodyH
	if ruled {
		avail -= weeks - 1
	}
	heights := m.monthRowHeights(avail)

	for w := 0; w < weeks; w++ {
		rowH := heights[w]
		if w > 0 && ruled {
			lines = append(lines, rule)
		}
		cells := make([][]string, 7)
		for d, cw := range cols {
			cells[d] = m.monthCell(w*7+d, cw, rowH)
		}
		_, isoWeek := m.month.start.AddDays(w * 7).In(m.loc).ISOWeek()
		for y := 0; y < rowH; y++ {
			num := ""
			if y == 0 {
				num = fmt.Sprintf("%2d", isoWeek)
			}
			row := make([]string, 7)
			for d := range cols {
				row[d] = cells[d][y]
			}
			lines = append(lines, numCell(num)+strings.Join(row, sep))
		}
	}
	return padLines(lines, height, width)
}

// monthRowHeights splits avail lines between the week rows: evenly, with the
// leftover lines going to the weeks whose busiest day has the most events.
func (m Model) monthRowHeights(avail int) []int {
	weeks := m.month.weeks
	heights := make([]int, weeks)
	need := make([]int, weeks)
	for w := range heights {
		heights[w] = max(avail/weeks, 1)
		for d := 0; d < 7; d++ {
			need[w] = max(need[w], len(m.month.days[w*7+d].Items))
		}
	}
	order := make([]int, weeks)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return need[order[a]] > need[order[b]] })
	for i := 0; i < avail%weeks; i++ {
		heights[order[i]]++
	}
	return heights
}

// monthCell renders grid day i (0-based from the grid start) as height lines
// of width cells: the date, then events.
func (m Model) monthCell(i, width, height int) []string {
	date := m.month.start.AddDays(i)
	inMonth := date.Month == m.month.month.Month
	items := m.month.days[i].Items

	// Date line: the day number, plus the month on the 1st and on the grid's
	// first cell so the eye can place leading/trailing days.
	label := fmt.Sprintf("%d", date.Day)
	if date.Day == 1 || i == 0 {
		if l := date.In(m.loc).Format("2 Jan"); ansi.StringWidth(l) <= width {
			label = l
		}
	}
	var dateLine string
	switch {
	case date == m.month.sel && m.focus == focusMain:
		dateLine = selectedStyle.Render(fit(label, width))
	case date == m.month.sel:
		dateLine = inactiveSelStyle.Render(fit(label, width))
	case date == m.today():
		dateLine = todayStyle.Render(fit(label, width))
	case !inMonth:
		dateLine = mutedStyle.Render(fit(label, width))
	default:
		dateLine = boldStyle.Render(fit(label, width))
	}
	lines := []string{dateLine}

	slots := height - 1
	shown := min(len(items), slots)
	if len(items) > slots {
		shown = max(slots-1, 0)
	}
	for _, it := range items[:shown] {
		lines = append(lines, m.monthEventLine(date, it, width, inMonth))
	}
	if hidden := len(items) - shown; hidden > 0 && slots > 0 {
		lines = append(lines, mutedStyle.Render(fit(moreLabel(hidden, shown == 0, width), width)))
	}
	return padLines(lines, height, width)
}

// moreLabel summarizes events that didn't fit: "+3 more", or "4 events" when
// none of the day's events could be shown, shortened to "+3"/"4" in narrow
// cells.
func moreLabel(n int, none bool, width int) string {
	long, short := fmt.Sprintf("+%d more", n), fmt.Sprintf("+%d", n)
	if none {
		long, short = fmt.Sprintf("%d events", n), fmt.Sprintf("%d", n)
	}
	if ansi.StringWidth(long) <= width {
		return long
	}
	return short
}

// monthWeekSpan is the grid's ISO week range, e.g. "Weeks 36–40".
func (m Model) monthWeekSpan() string {
	if m.weekStart != time.Monday {
		return ""
	}
	_, first := m.month.start.In(m.loc).ISOWeek()
	_, last := m.month.start.AddDays((m.month.weeks - 1) * 7).In(m.loc).ISOWeek()
	return fmt.Sprintf("Weeks %d–%d", first, last)
}

// monthEventLine is a bullet and the title, with the start time in front when
// the cell has room for it.
func (m Model) monthEventLine(date domain.CivilDate, it agenda.Item, width int, inMonth bool) string {
	slot := m.calendarSlot(it.Event.CalendarID)
	text := it.Event.Title
	if !it.Event.AllDay && width >= 14 && !it.Start.Before(date.In(m.loc)) {
		text = m.clock.Time(it.Start) + " " + text
	}
	if m.isPulsing(it.Event) {
		return m.pulseLine(it.Event, fit(calendarGlyph(slot, false)+text, width))
	}
	ended := !it.Event.AllDay && !it.End.After(m.now)
	if ended || !inMonth {
		return mutedStyle.Render(fit(calendarGlyph(slot, false)+text, width))
	}
	return fit(calendarBullet(slot)+text, width)
}
