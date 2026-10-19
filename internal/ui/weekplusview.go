package ui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// Drawing of Week+, the week as a time grid. From the top: a header with the
// days, a rule, a band for the all-day items, then the grid: a gutter with
// the hours, and a column of rows for each day. A row is a stretch of the
// clock (30 minutes by default), and an event is drawn over the rows it
// covers, with no background (the palette is the terminal's), so it is made of
// characters: its calendar's bullet, then an edge down the side.
//
// Events that overlap are put side by side in lanes (see agenda.BuildGrid), as
// many as the column has room for. The selected event is drawn over the whole
// column instead, so it can always be read, and the events beside it come back
// when the selection moves.

// wpGeom is the size of everything in the view, for a width×height area.
type wpGeom struct {
	w, h     int
	gutter   int   // the width of the hours column
	vis      []int // the days of the week shown, from the week's first
	first, n int   // the columns shown: vis[first : first+n]
	widths   []int // the width of each of those, a separator between them
	band     int   // rows of all-day items
	body     int   // rows of the grid
	tooSmall bool
}

func (m Model) wpGeometry() wpGeom { return m.wpGeometryIn(m.mainWidth()-2, m.bodyHeight()-2) }

func (m Model) wpGeometryIn(w, h int) wpGeom {
	g := wpGeom{w: w, h: h, gutter: 6, vis: m.wpVisible()}
	if m.clock.Twelve {
		g.gutter = 8
	}
	avail := w - g.gutter
	if avail < 8 || h < 5 {
		g.tooSmall = true
		return g
	}
	g.n = max(min((avail+1)/(wpMinDayCol+1), len(g.vis)), 1)
	sel := 0
	for i, d := range g.vis {
		if d == m.wp.day {
			sel = i
		}
	}
	g.first = max(min(m.wp.first, len(g.vis)-g.n), 0)
	if sel < g.first {
		g.first = sel
	} else if sel >= g.first+g.n {
		g.first = sel - g.n + 1
	}
	g.widths = columnWidths(avail, g.n)
	for _, d := range g.vis[g.first : g.first+g.n] {
		if d < len(m.wp.grids) {
			g.band = max(g.band, len(m.wp.grids[d].AllDay))
		}
	}
	g.band = min(g.band, wpBandMax)
	g.body = h - 2 - g.band
	return g
}

// wpSpan is the shown week's dates, e.g. "2 Nov – 8 Nov 2026".
func (m Model) wpSpan() string {
	start, end := m.wp.start, m.wp.start.AddDays(6)
	return start.In(m.loc).Format("2 Jan") + " – " + end.In(m.loc).Format("2 Jan 2006")
}

// wpWorking reports whether a time of day, in minutes, is in working hours.
func (m Model) wpWorking(mins int) bool { return mins >= m.workFrom && mins < m.workTo }

func (m Model) renderWeekPlus(width, height int) []string {
	g := m.wpGeometryIn(width, height)
	note := func(text string, style lipgloss.Style) []string {
		var lines []string
		for _, l := range strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n") {
			lines = append(lines, style.Render(fit(l, width)))
		}
		return padLines(lines, height, width)
	}
	switch {
	case g.tooSmall:
		return note("Too small for Week+. Make the window larger, or press 2 for the week.", mutedStyle)
	case !m.wp.loaded && m.loadErr != nil:
		return note("Can't read the local cache: "+m.loadErr.Error(), errorStyle)
	case !m.wp.loaded:
		return note("Loading…", mutedStyle)
	}

	cols := g.vis[g.first : g.first+g.n]
	sep := borderStyle.Render("│")
	blank := strings.Repeat(" ", g.gutter)

	var headers, rules []string
	for i, d := range cols {
		headers = append(headers, m.wpHeader(d, g.widths[i]))
		rules = append(rules, strings.Repeat("─", g.widths[i]))
	}
	up, down := blank, borderStyle.Render(strings.Repeat("─", g.gutter))
	if m.wp.top > 0 {
		up = mutedStyle.Render(fit("▲", g.gutter))
	}
	if m.wp.top+g.body < agenda.Rows(m.wpSlot()) {
		down = mutedStyle.Render("▼") + borderStyle.Render(strings.Repeat("─", g.gutter-1))
	}
	lines := []string{up + strings.Join(headers, sep), down + borderStyle.Render(strings.Join(rules, "┼"))}

	for r := 0; r < g.band; r++ {
		cells := make([]string, len(cols))
		for i, d := range cols {
			cells[i] = m.wpBandCell(d, r, g)
		}
		label := blank
		if r == 0 {
			label = mutedStyle.Render(fit("all", g.gutter))
		}
		lines = append(lines, label+strings.Join(cells, sep))
	}

	columns := make([][]string, len(cols))
	for i, d := range cols {
		columns[i] = m.wpColumn(d, g.widths[i], g)
	}
	for y := 0; y < g.body; y++ {
		cells := make([]string, len(cols))
		for i := range cols {
			cells[i] = columns[i][y]
		}
		lines = append(lines, m.wpGutter(m.wp.top+y, g)+strings.Join(cells, sep))
	}
	return padLines(lines, height, width)
}

func (m Model) wpHeader(d, width int) string {
	date := m.wp.start.AddDays(d)
	label := date.In(m.loc).Format("Mon 2")
	style := boldStyle
	if date == m.today() {
		style = todayStyle
	}
	if d == m.wp.day {
		if len(label)+1 <= width {
			label = "▸" + label
		}
		style = style.Underline(true)
	}
	return style.Render(fit(label, width))
}

// selStyle is how the selection looks: reversed in the focused view, only
// underlined when the keys go elsewhere.
func (m Model) selStyle() lipgloss.Style {
	if m.focus == focusMain {
		return selectedStyle
	}
	return inactiveSelStyle
}

// wpBandCell is row r of day d's all-day band.
func (m Model) wpBandCell(d, r int, g wpGeom) string {
	width := g.widths[0]
	for i, v := range g.vis[g.first : g.first+g.n] {
		if v == d {
			width = g.widths[i]
		}
	}
	items := m.wp.grids[d].AllDay
	selected := -1
	if d == m.wp.day && m.wp.pos >= 0 && m.wp.pos < len(items) {
		selected = m.wp.pos
	}
	i := r
	more := ""
	if len(items) > g.band && r == g.band-1 {
		// The last row says how many more there are, unless one of them is
		// selected: then it shows, so the selection is never out of sight.
		hidden := len(items) - (g.band - 1)
		if selected >= g.band-1 {
			i, more = selected, fmt.Sprintf(" +%d", hidden-1)
		} else {
			return mutedStyle.Render(fit(fmt.Sprintf("+%d more", hidden), width))
		}
	}
	if i >= len(items) {
		return strings.Repeat(" ", width)
	}
	e := items[i].Event
	plain := calendarGlyph(m.calendarSlot(e.CalendarID), false) + " " + oneLine(e.Title)
	switch {
	case i == selected:
		return m.selStyle().Render(fit(plain+more, width))
	case m.isPulsing(e):
		return m.pulseLine(e, fit(plain, width))
	}
	return fit(calendarBullet(m.calendarSlot(e.CalendarID))+" "+oneLine(e.Title), width)
}

// hourLabel is an hour, 0–23, as the gutter shows it.
func (m Model) hourLabel(h int) string {
	if !m.clock.Twelve {
		return fmt.Sprintf("%02d:00", h)
	}
	switch {
	case h == 0:
		return "12am"
	case h < 12:
		return fmt.Sprintf("%dam", h)
	case h == 12:
		return "12pm"
	}
	return fmt.Sprintf("%dpm", h-12)
}

// wpGutter is row r's hour label, or the time now when this is the row it is
// in, in the color of the line across the day.
func (m Model) wpGutter(r int, g wpGeom) string {
	slot := m.wpSlot()
	mins := r * slot
	if mins >= 24*60 {
		return strings.Repeat(" ", g.gutter)
	}
	if m.wpNowRow(r) {
		return nowStyle.Render(fit(m.clock.Time(m.now.In(m.loc)), g.gutter-1)) + " "
	}
	if mins%60 != 0 {
		return strings.Repeat(" ", g.gutter)
	}
	label := fit(m.hourLabel(mins/60), g.gutter-1) + " "
	if !m.wpWorking(mins) {
		return mutedStyle.Render(label)
	}
	return label
}

// wpNowRow reports whether row r is the one the time now is in, when today is
// a day shown.
func (m Model) wpNowRow(r int) bool {
	if i := daysBetween(m.wp.start, m.today()); i < 0 || i > 6 || m.wpHidden(m.today()) {
		return false
	}
	return agenda.MinutesOfDay(m.now.In(m.loc))/m.wpSlot() == r
}

type wpSeg struct {
	x0, x1 int
	text   string
}

// laneBounds is the columns [x0, x1) of the span lanes from lane, in a
// column w wide split in lanes lanes.
func laneBounds(w, lanes, lane, span int) (x0, x1 int) {
	edge := func(i int) int { return i*(w/lanes) + min(i, w%lanes) }
	return edge(lane), edge(lane + span)
}

// wpColumn draws day d's grid as exactly g.body lines w wide.
func (m Model) wpColumn(d, w int, g wpGeom) []string {
	grid := m.wp.grids[d]
	segs := make([][]wpSeg, g.body)
	maxLanes := max(min(w/wpMinLane, wpMaxLanes), 1)
	selected := -1
	if _, block, ok := m.wpItem(d, m.wp.pos); ok && d == m.wp.day {
		selected = block
	}

	marked := map[int]bool{} // clusters that have said how many are left out
	for i, b := range grid.Blocks {
		lanes := min(b.Lanes, maxLanes)
		if i == selected || b.Lane >= lanes {
			continue
		}
		more := 0
		if b.Lane == lanes-1 && !marked[b.Cluster] {
			more, marked[b.Cluster] = grid.Hidden(b.Cluster, lanes), true
		}
		x0, x1 := laneBounds(w, lanes, b.Lane, min(b.Span, lanes-b.Lane))
		for r := max(b.Row0, m.wp.top); r < min(b.Row1, m.wp.top+g.body); r++ {
			segs[r-m.wp.top] = append(segs[r-m.wp.top], wpSeg{x0, x1, m.wpBlockRow(b, r-b.Row0, x1-x0, false, more, grid.Date)})
		}
	}
	if selected >= 0 {
		// It covers the lanes beside it, so it also says how many events have
		// no lane, the ones it hides excepted.
		b := grid.Blocks[selected]
		more := grid.Hidden(b.Cluster, min(b.Lanes, maxLanes))
		if b.Lane >= min(b.Lanes, maxLanes) {
			more--
		}
		for r := max(b.Row0, m.wp.top); r < min(b.Row1, m.wp.top+g.body); r++ {
			segs[r-m.wp.top] = []wpSeg{{0, w, m.wpBlockRow(b, r-b.Row0, w, true, more, grid.Date)}}
		}
	}

	out := make([]string, g.body)
	for y := range out {
		if m.wp.top+y >= agenda.Rows(m.wpSlot()) {
			out[y] = strings.Repeat(" ", w) // past the end of the day
			continue
		}
		row := segs[y]
		sort.Slice(row, func(a, b int) bool { return row[a].x0 < row[b].x0 })
		var sb strings.Builder
		x := 0
		for _, s := range row {
			sb.WriteString(m.wpGap(d, m.wp.top+y, x, s.x0))
			sb.WriteString(s.text)
			x = s.x1
		}
		sb.WriteString(m.wpGap(d, m.wp.top+y, x, w))
		out[y] = sb.String()
	}
	return out
}

// wpGap fills the columns [x0, x1) of day d's row r where no event is: the
// line across the day at the time now, a faint line on the hour in working
// hours, and the marker of a selected time on a day with nothing.
func (m Model) wpGap(d, r, x0, x1 int) string {
	n := x1 - x0
	if n <= 0 {
		return ""
	}
	if d == m.wp.day && m.wp.pos < 0 && x0 == 0 && r == m.wp.at/m.wpSlot() {
		return accentStyle.Render("▸") + m.wpGapFill(d, r, n-1)
	}
	return m.wpGapFill(d, r, n)
}

func (m Model) wpGapFill(d, r, n int) string {
	if n <= 0 {
		return ""
	}
	switch {
	case m.wp.start.AddDays(d) == m.today() && m.wpNowRow(r):
		return nowStyle.Render(strings.Repeat("─", n))
	case r*m.wpSlot()%60 == 0 && m.wpWorking(r*m.wpSlot()):
		return mutedStyle.Render(strings.Repeat("┈", n))
	}
	return strings.Repeat(" ", n)
}

// wpBlockRow is row k of a block, width wide. The first row has the
// calendar's bullet in the edge and the title; a taller block's second row has
// its time, and the rest are the edge. more is how many events are left out
// of the lanes beside it.
func (m Model) wpBlockRow(b agenda.Block, k, width int, selected bool, more int, date domain.CivilDate) string {
	e := b.Item.Event
	rows := b.Row1 - b.Row0
	title := oneLine(e.Title)
	slot := m.calendarSlot(e.CalendarID)

	edge, text := "│", ""
	switch {
	case k == 0:
		edge = calendarGlyph(slot, false)
		if rows == 1 || b.Instant {
			label := ""
			switch {
			case b.FromPrev:
				label = "▲"
			case width >= 8+len(m.clock.Time(b.Item.Start)) || b.Instant:
				label = m.clock.Time(b.Item.Start)
			}
			text = strings.TrimSpace(label + " " + title)
			if b.ToNext {
				text += " ▼"
			}
		} else {
			text = title
			if b.FromPrev {
				text = "▲ " + text
			}
		}
	case k == 1:
		text = b.Item.TimeLabel(date, m.loc, m.clock)
		if b.ToNext && rows == 2 {
			text += " ▼"
		}
	case k == rows-1 && b.ToNext && rows >= 3:
		text = "▼"
	}
	if selected && k > 0 {
		edge = "┃"
	}
	suffix := ""
	if more > 0 && k == 0 {
		suffix = fmt.Sprintf("+%d", more)
	}
	left := fit(edge+" "+text, width-len(suffix))
	tail := mutedStyle.Render(suffix)

	switch {
	case selected && k == 0:
		return m.selStyle().Render(left) + tail
	case selected:
		return boldStyle.Render(left) + tail
	case k == 0 && m.isPulsing(e):
		return m.pulseLine(e, left) + tail
	case !b.Item.End.After(m.now):
		return mutedStyle.Render(left) + tail
	case k == 0:
		return fit(calendarBullet(slot)+" "+text, width-len(suffix)) + tail
	}
	return mutedStyle.Render(left) + tail
}
