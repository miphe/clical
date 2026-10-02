package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

const minWeekColumn = 6 // narrower than this and the week view gives up

// renderWeek draws the week as seven day columns for a width×height area
// inside the main panel (whose border carries the week's title). Each event
// takes two lines: a colored bullet with its time, then its title.
func (m Model) renderWeek(width, height int) []string {
	cols := columnWidths(width, 7)
	if cols[0] < minWeekColumn {
		msg := lipgloss.NewStyle().Width(width).Render("Too narrow for the week view. Widen the window, or press 1 for the agenda.")
		lines := strings.Split(msg, "\n")
		for i := range lines {
			lines[i] = dimStyle.Render(fit(lines[i], width))
		}
		return padLines(lines, height, width)
	}

	if !m.week.loaded {
		line := dimStyle.Render("Loading…")
		if m.loadErr != nil {
			line = errorStyle.Render("Can't read the local cache: " + m.loadErr.Error())
		}
		return padLines([]string{fit(line, width)}, height, width)
	}

	var lines []string

	var headers, rules []string
	for d, w := range cols {
		headers = append(headers, m.weekHeader(d, w))
		rules = append(rules, strings.Repeat("─", w))
	}
	sep := borderStyle.Render("│")
	lines = append(lines,
		strings.Join(headers, sep),
		borderStyle.Render(strings.Join(rules, "┼")))

	bodyH := height - len(lines)
	columns := make([][]string, 7)
	for d, w := range cols {
		columns[d] = m.weekColumn(d, w, bodyH)
	}
	for y := 0; y < bodyH; y++ {
		cells := make([]string, 7)
		for d := range cols {
			cells[d] = columns[d][y]
		}
		lines = append(lines, strings.Join(cells, sep))
	}
	return padLines(lines, height, width)
}

// columnWidths splits total (minus one separator between each column) into
// n widths, handing the remainder to the leftmost columns.
func columnWidths(total, n int) []int {
	avail := total - (n - 1)
	out := make([]int, n)
	for i := range out {
		out[i] = avail / n
		if i < avail%n {
			out[i]++
		}
	}
	return out
}

func (m Model) weekTitle() string {
	start, end := m.week.start, m.week.start.AddDays(6)
	_, week := start.In(m.loc).ISOWeek()
	span := start.In(m.loc).Format("2 Jan") + " – " + end.In(m.loc).Format("2 Jan 2006")
	if weekFirstDay == time.Monday {
		return fmt.Sprintf("Week %d · %s", week, span)
	}
	return span
}

func (m Model) weekHeader(d, width int) string {
	date := m.week.start.AddDays(d)
	label := date.In(m.loc).Format("Mon 2")
	style := boldStyle
	if date == m.today() {
		style = todayStyle
	}
	if d == m.week.day {
		if ansi.StringWidth("▸"+label) <= width {
			label = "▸" + label
		}
		style = style.Underline(true)
	}
	return style.Render(fit(label, width))
}

// weekColumn renders one day's events into exactly height lines. The
// selected column scrolls so its selection stays visible; hidden events are
// summarized as "+N more".
func (m Model) weekColumn(d, width, height int) []string {
	date := m.week.start.AddDays(d)
	items := m.week.days[d].Items
	selected := d == m.week.day && m.focus == focusMain

	var lines []string
	if len(items) == 0 {
		text := "–"
		if d == m.week.day {
			text = "nothing"
		}
		style := dimStyle
		if selected {
			style = selectedStyle
		}
		lines = append(lines, style.Render(fit(text, width)))
		return padLines(lines, height, width)
	}

	capacity := height / 2
	start := 0
	if d == m.week.day && m.week.item >= capacity {
		start = m.week.item - capacity + 1
	}
	end := min(start+capacity, len(items))
	if end < len(items) {
		// Make room for the "+N more" line.
		end = max(start, end-1)
		if d == m.week.day && m.week.item >= end && end > start {
			start++
			end++
		}
	}

	for i := start; i < end; i++ {
		lines = append(lines, m.weekItemLines(date, items[i], width, selected && i == m.week.item, d == m.week.day && i == m.week.item)...)
	}
	if hidden := len(items) - end; hidden > 0 {
		lines = append(lines, dimStyle.Render(fit(fmt.Sprintf("+%d more", hidden), width)))
	}
	return padLines(lines, height, width)
}

// bulletMinWidth is the narrowest column that still gets a "● " bullet;
// below it the time itself carries the calendar's color.
const bulletMinWidth = 9

func (m Model) weekItemLines(date domain.CivilDate, it agenda.Item, width int, active, inactiveSel bool) []string {
	when := shortTimeLabel(date, it, m.loc)
	bullet := width >= bulletMinWidth
	plainWhen := when
	if bullet {
		plainWhen = "● " + when
	}

	switch {
	case active:
		return []string{selectedStyle.Render(fit(plainWhen, width)), selectedStyle.Render(fit(it.Event.Title, width))}
	case inactiveSel:
		return []string{inactiveSelStyle.Render(fit(plainWhen, width)), inactiveSelStyle.Render(fit(it.Event.Title, width))}
	case !it.Event.AllDay && !it.End.After(m.now):
		return []string{dimStyle.Render(fit(plainWhen, width)), dimStyle.Render(fit(it.Event.Title, width))}
	}

	color := lipgloss.NewStyle().Foreground(calendarColor(m.colors[it.Event.CalendarID]))
	first := color.Render(fit(when, width))
	if bullet {
		first = fit(color.Render("●")+" "+dimStyle.Render(when), width)
	}
	return []string{first, fit(it.Event.Title, width)}
}

// shortTimeLabel fits a narrow week column: "all day", the start time, or
// "…HH:MM" for an event continuing from the day before.
func shortTimeLabel(date domain.CivilDate, it agenda.Item, loc *time.Location) string {
	if it.Event.AllDay {
		return "all day"
	}
	if it.Start.Before(date.In(loc)) {
		if it.End.Equal(date.AddDays(1).In(loc)) || it.End.After(date.AddDays(1).In(loc)) {
			return "all day…"
		}
		return "…" + it.End.Format("15:04")
	}
	return it.Start.Format("15:04")
}
