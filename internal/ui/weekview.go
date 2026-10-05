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
			lines[i] = mutedStyle.Render(fit(lines[i], width))
		}
		return padLines(lines, height, width)
	}

	if !m.week.loaded {
		line := mutedStyle.Render("Loading…")
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

// weekNumber is the ISO week number of the shown week ("week 45", as it's
// commonly referred to in Sweden and much of Europe), or 0 when weeks don't
// start on Monday and ISO numbering doesn't apply.
func (m Model) weekNumber() int {
	if m.weekStart != time.Monday {
		return 0
	}
	_, week := m.week.start.In(m.loc).ISOWeek()
	return week
}

// weekSpan is the shown week's dates, e.g. "2 Nov – 8 Nov 2026".
func (m Model) weekSpan() string {
	start, end := m.week.start, m.week.start.AddDays(6)
	return start.In(m.loc).Format("2 Jan") + " – " + end.In(m.loc).Format("2 Jan 2006")
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
		style := mutedStyle
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
		lines = append(lines, mutedStyle.Render(fit(fmt.Sprintf("+%d more", hidden), width)))
	}
	return padLines(lines, height, width)
}

// bulletMinWidth is the narrowest column that gets a space between bullet
// and time; below it they're joined ("●09:00") to save a cell.
const bulletMinWidth = 9

func (m Model) weekItemLines(date domain.CivilDate, it agenda.Item, width int, active, inactiveSel bool) []string {
	when := shortTimeLabel(date, it, m.loc, m.clock)
	slot := m.calendarSlot(it.Event.CalendarID)
	gap := " "
	if width < bulletMinWidth {
		gap = ""
	}
	plainWhen := calendarGlyph(slot, false) + gap + when

	switch {
	case m.isPulsing(it.Event):
		return []string{m.pulseLine(it.Event, fit(plainWhen, width)), m.pulseLine(it.Event, fit(it.Event.Title, width))}
	case active:
		return []string{selectedStyle.Render(fit(plainWhen, width)), selectedStyle.Render(fit(it.Event.Title, width))}
	case inactiveSel:
		return []string{inactiveSelStyle.Render(fit(plainWhen, width)), inactiveSelStyle.Render(fit(it.Event.Title, width))}
	case !it.Event.AllDay && !it.End.After(m.now):
		return []string{mutedStyle.Render(fit(plainWhen, width)), mutedStyle.Render(fit(it.Event.Title, width))}
	}

	return []string{fit(calendarBullet(slot)+gap+mutedStyle.Render(when), width), fit(it.Event.Title, width)}
}

// shortTimeLabel fits a narrow week column: "all day", the start time, or
// "…HH:MM" for an event continuing from the day before.
func shortTimeLabel(date domain.CivilDate, it agenda.Item, loc *time.Location, c agenda.Clock) string {
	if it.Event.AllDay {
		return "all day"
	}
	if it.Start.Before(date.In(loc)) {
		if it.End.Equal(date.AddDays(1).In(loc)) || it.End.After(date.AddDays(1).In(loc)) {
			return "all day…"
		}
		return "…" + c.Time(it.End)
	}
	return c.Time(it.Start)
}
