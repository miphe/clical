package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// The Today panel of Focus+ (key 5): what is on now and next, the day as a
// strip of cells, and the agenda's rows with every title in full. It is made
// to be glanced at while the app is left open: the top lines answer "what
// now, what next" and the strip the shape of the day, so the list below
// never has to cut a title to fit the time of day.

const (
	todayStripH   = 3  // hour labels, cells, the marker row
	todayMinStrip = 20 // narrower than this, the strip is left out
	todayWideW    = 60 // from here a now/next line holds its times too
	todayRangeW   = 40 // from here a list row shows its range, not its start
	todayFreeMin  = 30 // gaps this long or longer get a "free" line
)

// todayDays is today and tomorrow, of the calendars shown: what now and next
// are found in. The text filter doesn't apply; the glance is the whole day.
func (m Model) todayDays() []agenda.Day {
	var visible []domain.Event
	for _, e := range m.events {
		if m.shows(e) {
			visible = append(visible, e)
		}
	}
	return agenda.Build(visible, m.today(), 2, m.loc)
}

// todayGrid is today laid out against the clock.
func (m Model) todayGrid(days []agenda.Day) agenda.DayGrid {
	if len(days) == 0 {
		return agenda.DayGrid{Date: m.today()}
	}
	return agenda.BuildGrid(days[0], m.loc, 30)
}

// shortDur is a duration as the panel says it: "8 min", "1h", "1h05".
func shortDur(d time.Duration) string {
	mins := int((d + time.Minute - 1) / time.Minute)
	if mins < 60 {
		return fmt.Sprintf("%d min", max(mins, 1))
	}
	if mins%60 == 0 {
		return fmt.Sprintf("%dh", mins/60)
	}
	return fmt.Sprintf("%dh%02d", mins/60, mins%60)
}

// itemRange is an item's times, "09:22–09:52", within today.
func (m Model) itemRange(it agenda.Item) string {
	return it.TimeLabel(m.today(), m.loc, m.clock)
}

// whenNext says when a later item starts: "in 8 min" today, "tomorrow
// 09:30" or "Fri 09:30" after.
func (m Model) whenNext(it agenda.Item) string {
	day := domain.CivilDateFromTime(it.Start.In(m.loc))
	switch {
	case day == m.today():
		return "in " + shortDur(it.Start.Sub(m.now))
	case day == m.today().AddDays(1):
		return "tomorrow " + m.clock.Time(it.Start.In(m.loc))
	}
	return it.Start.In(m.loc).Format("Mon") + " " + m.clock.Time(it.Start.In(m.loc))
}

// todayHeader is the now and next lines for a width: each a label, the
// event's bullet and its title in full (wrapped), and its times. Wide, the
// times sit on the right of the title's first line; narrow, on a line of
// their own.
func (m Model) todayHeader(width int) []string { return m.todayHeaderLines(width, 2) }

// todayHeaderFit is the header in at most room rows when it can be: each
// title on one line if two don't fit.
func (m Model) todayHeaderFit(width, room int) []string {
	if h := m.todayHeader(width); len(h) <= room {
		return h
	}
	return m.todayHeaderLines(width, 1)
}

// todayHeaderLines is the header with each title in at most titleLines rows.
func (m Model) todayHeaderLines(width, titleLines int) []string {
	cur, next := agenda.NowNext(m.todayDays(), m.now)
	const labelW = 6 // "now   ", "next  "
	var out []string

	line := func(label string, it *agenda.Item, title, info string) {
		lead := boldStyle.Render(fit(label, labelW))
		bullet := ""
		if it != nil {
			bullet = calendarBullet(m.calendarSlot(it.Event.CalendarID)) + " "
		}
		pre := labelW + ansi.StringWidth(ansi.Strip(bullet))
		infoW := ansi.StringWidth(info)
		wide := width >= todayWideW && info != ""
		titleW := width - pre
		if wide {
			titleW -= infoW + 2
		}
		lines := wrapped(title, max(titleW, 1))
		if len(lines) > titleLines {
			// Up here a title is cut short; the list below has it in full.
			keep := append([]string{}, lines[:titleLines-1]...)
			lines = append(keep, ansi.Truncate(strings.Join(lines[titleLines-1:], " "), max(titleW, 1), "…"))
		}
		pad := strings.Repeat(" ", pre)
		for i, l := range lines {
			text := l
			if it != nil && i == 0 && m.isPulsing(it.Event) {
				text = m.pulseLine(it.Event, l)
			}
			s := pad + text
			if i == 0 {
				s = lead + bullet + text
			}
			if i == 0 && wide {
				s = fit(s, width-infoW) + mutedStyle.Render(info)
			}
			out = append(out, s)
		}
		if !wide && info != "" {
			out = append(out, pad+mutedStyle.Render(info))
		}
	}

	switch {
	case len(cur) > 0:
		// The one that started last is the one you're most likely in.
		it := cur[len(cur)-1]
		info := m.itemRange(it) + " · " + shortDur(it.End.Sub(m.now)) + " left"
		if len(cur) > 1 {
			info += fmt.Sprintf(" · +%d", len(cur)-1)
		}
		line("now", &it, domain.SafeText(oneLine(it.Event.Title)), info)
	case len(next) > 0 && domain.CivilDateFromTime(next[0].Start.In(m.loc)) == m.today():
		line("now", nil, mutedStyle.Render("free until "+m.clock.Time(next[0].Start.In(m.loc))),
			shortDur(next[0].Start.Sub(m.now)))
	default:
		line("now", nil, mutedStyle.Render("nothing more today"), "")
	}
	if len(next) > 0 {
		it := next[0]
		info := m.itemRange(it) + " · " + m.whenNext(it)
		if domain.CivilDateFromTime(it.Start.In(m.loc)) != m.today() {
			info = m.whenNext(it)
		}
		if len(next) > 1 {
			info += fmt.Sprintf(" · +%d", len(next)-1)
		}
		line("next", &it, domain.SafeText(oneLine(it.Event.Title)), info)
	} else {
		line("next", nil, mutedStyle.Render("nothing coming up"), "")
	}
	return out
}

// todayStrip draws today as three rows: hour labels, a cell per stretch of
// the clock, and a marker row with ▲ under now and └─┘ under the selected
// event. A busy cell is ━ in its calendar's color, ═ where events overlap;
// a free one is ┄ in working hours and blank outside. Cells before now are
// faint. Without color the shapes still say it all.
func (m Model) todayStrip(width int) []string {
	g := m.todayGrid(m.todayDays())
	from, to := agenda.StripRange(g, m.workFrom, m.workTo)
	step := agenda.StripStep(from, to, width)
	cells := agenda.Strip(g, from, to, step)
	if len(cells) > width {
		cells = cells[:width]
	}
	nowMin := agenda.MinutesOfDay(m.now.In(m.loc))

	// Hour labels, every hour that fits with a space after the last.
	labels := []rune(strings.Repeat(" ", len(cells)))
	perHour := 60 / step
	every := 1
	for every*perHour < 3 { // "06 " needs three cells
		every++
	}
	for i, c := range cells {
		if c.Start%60 != 0 || (c.Start/60)%every != 0 {
			continue
		}
		label := fmt.Sprintf("%02d", c.Start/60)
		if i+len(label) > len(cells) {
			continue
		}
		for k, r := range label {
			labels[i+k] = r
		}
	}

	var bar strings.Builder
	for _, c := range cells {
		past := c.Start+step <= nowMin
		switch {
		case c.Count == 0 && c.Start >= m.workFrom && c.Start < m.workTo:
			bar.WriteString(mutedStyle.Render("┄"))
		case c.Count == 0:
			bar.WriteString(" ")
		default:
			glyph := "━"
			if c.Count > 1 {
				glyph = "═"
			}
			bar.WriteString(stripMark(m.calendarSlot(c.First.Event.CalendarID), glyph, past))
		}
	}

	// The marker row: the selected event's span, then now on top of it.
	marks := []rune(strings.Repeat(" ", len(cells)))
	if it, ok := m.selectedTodayItem(); ok {
		s, e := agenda.MinutesOfDay(it.Start.In(m.loc)), agenda.MinutesOfDay(it.End.In(m.loc))
		if domain.CivilDateFromTime(it.Start.In(m.loc)).Before(m.today()) {
			s = 0
		}
		if !domain.CivilDateFromTime(it.End.In(m.loc)).Before(m.today().AddDays(1)) || e < s {
			e = 1440
		}
		var in []int
		for i, c := range cells {
			if c.Start < max(e, s+1) && c.Start+step > s {
				in = append(in, i)
			}
		}
		switch len(in) {
		case 0:
		case 1:
			marks[in[0]] = '^'
		default:
			for _, i := range in {
				marks[i] = '─'
			}
			marks[in[0]], marks[in[len(in)-1]] = '└', '┘'
		}
	}
	nowAt := -1
	if nowMin >= from && nowMin < to {
		nowAt = min((nowMin-from)/step, len(cells)-1)
	}
	var mk strings.Builder
	for i, r := range marks {
		switch {
		case i == nowAt:
			mk.WriteString(nowStyle.Render("▲"))
		case r != ' ':
			mk.WriteString(boldStyle.Render(string(r)))
		default:
			mk.WriteRune(' ')
		}
	}
	return []string{mutedStyle.Render(string(labels)), bar.String(), mk.String()}
}

// selectedTodayItem is the selected agenda item, if it is a timed item of
// today.
func (m Model) selectedTodayItem() (agenda.Item, bool) {
	i := m.selIndex()
	if i < 0 || m.rows[i].kind != rowItem || m.rows[i].date != m.today() || m.rows[i].item.Event.AllDay {
		return agenda.Item{}, false
	}
	return m.rows[i].item, true
}

// todayRowLines is what an agenda row draws in the Today panel: like the
// agenda's, but the title wraps instead of being cut, the event on now is
// marked ▶, and later days follow as they do there.
func (m Model) todayRowLines(r row, selected bool, width int) []string {
	if r.kind != rowItem {
		return []string{m.renderRow(r, selected, width)}
	}
	it := r.item
	when := it.TimeLabel(r.date, m.loc, m.clock)
	whenW := m.clock.Width()
	if width < todayRangeW {
		when = strings.SplitN(when, "–", 2)[0]
		whenW = compactWhenW
	}
	labelW := 0
	label := m.calendarLabel(it.Event.CalendarID)
	if width >= todayWideW {
		labelW = m.labelWidth()
	}
	on := !it.Event.AllDay && !it.Start.After(m.now) && it.End.After(m.now)
	mark := " "
	if on {
		mark = "▶"
	}
	pre := 1 + 2 + whenW + 2 // mark, bullet and space, the time, a gap
	titleW := width - pre
	if labelW > 0 {
		titleW -= labelW + 2
	}
	title := wrapped(domain.SafeText(oneLine(it.Event.Title)), max(titleW, 1))

	slot := m.calendarSlot(it.Event.CalendarID)
	ended := !it.Event.AllDay && !it.End.After(m.now)
	var out []string
	for i, t := range title {
		var s string
		if i == 0 {
			s = calendarGlyph(slot, false) + " " + fit(when, whenW) + "  " + fit(t, max(titleW, 1))
			if labelW > 0 {
				s += "  " + fit(label, labelW)
			}
		} else {
			s = strings.Repeat(" ", pre-1) + fit(t, max(titleW, 1))
		}
		body := width - 1
		switch {
		case i == 0 && m.isPulsing(it.Event):
			s = m.pulseLine(it.Event, s)
		case selected && m.focus == focusMain:
			s = selectedStyle.Render(fit(s, body))
		case selected:
			s = inactiveSelStyle.Render(fit(s, body))
		case ended:
			s = mutedStyle.Render(s)
		case i == 0:
			// The bullet in its color, the rest plain, the label muted.
			s = calendarBullet(slot) + " " + fit(when, whenW) + "  " + fit(t, max(titleW, 1))
			if labelW > 0 {
				s += "  " + mutedStyle.Render(fit(label, labelW))
			}
		}
		lead := " "
		if i == 0 && on {
			lead = accentStyle.Render(mark)
		}
		out = append(out, fit(lead+s, width))
	}
	return out
}

// todayLines lays out every row from first on, with a muted "free" line
// before a later event of today that leaves a gap of half an hour or more.
// start[i] is the first line of row first+i.
func (m Model) todayLines(first, width int) (lines []string, start []int) {
	sel := m.selIndex()
	today := m.today()
	busyTo := m.now // the end of what's on or ended, so far
	for i := 0; i < first && i < len(m.rows); i++ {
		if r := m.rows[i]; r.kind == rowItem && r.date == today && !r.item.Event.AllDay && r.item.End.After(busyTo) {
			busyTo = r.item.End
		}
	}
	for i := first; i < len(m.rows); i++ {
		r := m.rows[i]
		if r.kind == rowItem && r.date == today && !r.item.Event.AllDay {
			if gap := r.item.Start.Sub(busyTo); r.item.Start.After(m.now) && gap >= todayFreeMin*time.Minute {
				lines = append(lines, fit("      "+mutedStyle.Render("free "+shortDur(gap)), width))
			}
			if r.item.End.After(busyTo) {
				busyTo = r.item.End
			}
		}
		start = append(start, len(lines))
		lines = append(lines, m.todayRowLines(r, i == sel, width)...)
	}
	return lines, start
}

// todayListHeight is the room for the list in the Today panel.
func (m Model) todayListHeight() int {
	lay := m.focusPlusLayout()
	if lay.todayCompact {
		return 0
	}
	return max(lay.todayH-2-m.todayTopH(m.todayContentW()), 0)
}

// todayTopH is how many rows the now/next lines and the strip take, with a
// blank row under them, in a panel of inner width.
func (m Model) todayTopH(inner int) int {
	h := len(m.todayHeader(inner))
	if inner >= todayMinStrip {
		h += todayStripH
	}
	return h + 1
}

// ensureTodayVisible scrolls the Today list so the selection shows, whole.
// A selection that lands far below the view (a fresh load, t, a day jump)
// is brought near the top with one row above it, so what's next leads; a
// step down scrolls only as far as needed.
func (m *Model) ensureTodayVisible() {
	h := m.todayListHeight()
	i := m.selIndex()
	if h <= 0 || i < 0 {
		return
	}
	lines, start := m.todayLines(0, m.todayContentW())
	end := func(k int) int { // the line after row k
		if k+1 < len(start) {
			return start[k+1]
		}
		return len(lines)
	}
	m.offset = min(max(m.offset, 0), len(m.rows)-1)
	top := start[m.offset]
	switch {
	case i < m.offset:
		m.offset = i
		header := i
		for header > 0 && m.rows[header].kind != rowDay {
			header--
		}
		if end(i)-start[header] <= h {
			m.offset = header
		}
	case end(i) > top+h:
		if start[i] >= top+2*h {
			// Far below: one row above it, if it then fits.
			m.offset = max(i-1, 0)
			if m.rows[m.offset].kind == rowBlank {
				m.offset = i
			}
		}
		for m.offset < i && end(i) > start[m.offset]+h {
			m.offset++
		}
	}
	// Don't leave empty room below the last row.
	for m.offset > 0 && len(lines)-start[m.offset-1] <= h {
		m.offset--
	}
}
