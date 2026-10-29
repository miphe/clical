package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// The week view is the week as a time grid: a column per day and the time of
// day down the side (see weekview.go for how it is drawn). This file is its
// state, loading and keys. It reads one week of events at a time.
//
// What the user has selected is a time of day (at) and an entry of the day:
// the day's all-day items, then its timed blocks in order. Moving with j/k
// goes from entry to entry, but the time is what stays when changing day: h
// and l land on whatever is at that time. On a day with nothing, the time
// alone is the selection, and j/k move it, which is also where choosing a
// free slot would start.

const (
	weekMinDayCol = 14 // the narrowest day column that still gets a neighbour
	weekMaxLanes  = 3  // events side by side at most; the rest are counted
	weekMinLane   = 6  // the narrowest lane worth drawing
	weekBandMax   = 3  // rows of all-day events above the grid
	weekDefaultAt = 8  // the hour shown first when nothing says otherwise
)

// weekSlots are the zoom steps, in minutes a row, coarsest first.
var weekSlots = []int{60, 30, 15}

type weekState struct {
	start   domain.CivilDate
	loaded  bool // events hold start's week
	loading bool
	events  []domain.Event
	grids   []agenda.DayGrid // derived: the visible events, a grid for each day

	day  int  // selected day, 0–6 from start
	pos  int  // selected entry of the day (see weekEntries), -1 for none
	at   int  // the selected time of day, in minutes
	slot int  // minutes a row; 0 means the default
	top  int  // the first row shown
	five bool // weekdays only
	// first is the leftmost day column shown, as an index into the visible
	// days, when the window has room for fewer than the week.
	first int

	// pendingSelect: the next load selects pendingID in the selected day, or
	// else what is at the time; pendingNow aims at the time right now, and
	// fit scrolls to show it (or the day's first event).
	pendingSelect bool
	pendingID     string
	pendingNow    bool
	fit           bool
}

type weekLoadedMsg struct {
	start  domain.CivilDate
	events []domain.Event
	err    error
}

func (m Model) weekSlot() int {
	if m.week.slot == 0 {
		return 30
	}
	return m.week.slot
}

func (m Model) loadWeek() tea.Cmd {
	src, loc, start := m.src, m.loc, m.week.start
	return func() tea.Msg {
		events, err := src.Events(context.Background(), start.In(loc), start.AddDays(7).In(loc))
		return weekLoadedMsg{start: start, events: events, err: err}
	}
}

// weekHidden reports whether the weekday of d is left out of a weekdays-only week.
func (m Model) weekHidden(d domain.CivilDate) bool {
	wd := d.Weekday()
	return m.week.five && (wd == time.Saturday || wd == time.Sunday)
}

// weekVisible is the days of the week shown, as indexes from the first.
func (m Model) weekVisible() []int {
	var vis []int
	for i := 0; i < 7; i++ {
		if !m.weekHidden(m.week.start.AddDays(i)) {
			vis = append(vis, i)
		}
	}
	return vis
}

func (m Model) weekDate() domain.CivilDate { return m.week.start.AddDays(m.week.day) }

// showWeek switches to the week containing day and selects day and, if
// found there, eventID. fit scrolls to the time now, or to the day's events.
func (m Model) showWeek(day domain.CivilDate, eventID string, fit bool) (tea.Model, tea.Cmd) {
	m.view = viewWeek
	m.focus = focusMain
	start := agenda.WeekStart(day, m.weekStart)
	m.week.day = daysBetween(start, day)
	m.week.pendingSelect, m.week.pendingID = true, eventID
	m.week.pendingNow = fit && eventID == "" && day == m.today()
	m.week.fit = m.week.fit || fit

	if m.week.loaded && start == m.week.start {
		m.applyWeekSelection()
		return m, nil
	}
	m.week.start = start
	m.week.loaded = false
	m.week.loading = true
	return m, m.loadWeek()
}

func (m Model) weekLoaded(msg weekLoadedMsg) Model {
	if msg.start != m.week.start {
		return m // a newer load is in flight
	}
	m.week.loading = false
	m.loadErr = msg.err
	if msg.err != nil {
		return m
	}
	m.week.events = msg.events
	m.week.loaded = true
	m.rebuildWeek()
	m.applyWeekSelection()
	return m
}

// rebuildWeek lays the week's visible events out again.
func (m *Model) rebuildWeek() {
	if !m.week.loaded {
		return
	}
	var visible []domain.Event
	for _, e := range m.week.events {
		if m.shows(e) {
			visible = append(visible, e)
		}
	}
	days := agenda.Build(visible, m.week.start, 7, m.loc)
	m.week.grids = make([]agenda.DayGrid, len(days))
	for i, d := range days {
		m.week.grids[i] = agenda.BuildGrid(d, m.loc, m.weekSlot())
	}
	m.week.pos = min(m.week.pos, m.weekEntries(m.week.day)-1)
}

// weekEntries is how many things can be selected on day d: its all-day items,
// then its timed blocks.
func (m Model) weekEntries(d int) int {
	if d < 0 || d >= len(m.week.grids) {
		return 0
	}
	g := m.week.grids[d]
	return len(g.AllDay) + len(g.Blocks)
}

// weekItem is the item at entry pos of day d, and its block (-1 for an
// all-day item).
func (m Model) weekItem(d, pos int) (it agenda.Item, block int, ok bool) {
	if d < 0 || d >= len(m.week.grids) || pos < 0 || pos >= m.weekEntries(d) {
		return agenda.Item{}, -1, false
	}
	g := m.week.grids[d]
	if pos < len(g.AllDay) {
		return g.AllDay[pos], -1, true
	}
	return g.Blocks[pos-len(g.AllDay)].Item, pos - len(g.AllDay), true
}

func (m Model) selectedWeekItem() (agenda.Item, bool) {
	it, _, ok := m.weekItem(m.week.day, m.week.pos)
	return it, ok
}

func (m Model) selectedWeekEventID() string {
	if it, ok := m.selectedWeekItem(); ok {
		return it.Event.ID
	}
	return ""
}

// weekPick is the entry of day d that is at time at: the block covering it,
// else the block starting nearest, else the first all-day item, else none.
func (m Model) weekPick(d, at int) int {
	if d < 0 || d >= len(m.week.grids) {
		return -1
	}
	g := m.week.grids[d]
	row, best, bestGap := at/g.Slot, -1, 1<<30
	for i, b := range g.Blocks {
		if b.Row0 <= row && row < b.Row1 {
			return len(g.AllDay) + i
		}
		if gap := abs(b.Start - at); gap < bestGap {
			best, bestGap = i, gap
		}
	}
	switch {
	case best >= 0:
		return len(g.AllDay) + best
	case len(g.AllDay) > 0:
		return 0
	}
	return -1
}

func daysBetween(from, to domain.CivilDate) int {
	return int(to.In(time.UTC).Sub(from.In(time.UTC)).Hours() / 24)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// weekSetPos selects an entry of the selected day and takes its time.
func (m *Model) weekSetPos(pos int) {
	m.week.pos = pos
	if _, block, ok := m.weekItem(m.week.day, pos); ok && block >= 0 {
		m.week.at = m.week.grids[m.week.day].Blocks[block].Start
	}
}

// applyWeekSelection resolves a pending selection once the week is loaded.
func (m *Model) applyWeekSelection() {
	if !m.week.pendingSelect || !m.week.loaded {
		return
	}
	m.week.pendingSelect = false
	if m.weekHidden(m.weekDate()) {
		m.week.day = m.weekNearestVisible(m.week.day)
	}
	if m.week.pendingNow {
		m.week.at = agenda.MinutesOfDay(m.now.In(m.loc))
	}
	m.week.pos = -1
	if id := m.week.pendingID; id != "" {
		for i := 0; i < m.weekEntries(m.week.day); i++ {
			if it, _, _ := m.weekItem(m.week.day, i); it.Event.ID == id {
				m.weekSetPos(i)
				break
			}
		}
	}
	if m.week.pos < 0 {
		m.week.pos = m.weekPick(m.week.day, m.week.at)
	}
	if m.week.fit {
		m.week.fit = false
		m.weekFit()
	}
	m.weekReveal()
}

// weekNearestVisible is the visible day nearest to day index d, the later one
// when they are equally near.
func (m Model) weekNearestVisible(d int) int {
	best, gap := d, 1<<30
	for _, v := range m.weekVisible() {
		if g := abs(v - d); g < gap || (g == gap && v > best) {
			best, gap = v, g
		}
	}
	return best
}

// weekFit scrolls to where the week's day starts: around the time now when the
// week is this one, else just above the earliest event of the days shown, else
// the usual start of a working day.
func (m *Model) weekFit() {
	g := m.weekGeometry()
	slot, rows := m.weekSlot(), agenda.Rows(m.weekSlot())
	top := weekDefaultAt * 60 / slot
	if today := daysBetween(m.week.start, m.today()); today >= 0 && today < 7 {
		top = agenda.MinutesOfDay(m.now.In(m.loc))/slot - g.body/3
	} else {
		first, found := rows, false
		for _, d := range m.weekVisible() {
			if d < len(m.week.grids) && len(m.week.grids[d].Blocks) > 0 {
				first, found = min(first, m.week.grids[d].Blocks[0].Row0), true
			}
		}
		if found {
			top = first - 1
		}
	}
	m.week.top = max(min(top, rows-g.body), 0)
}

// weekReveal scrolls, and slides the day columns, to keep the selection in view.
func (m *Model) weekReveal() {
	g := m.weekGeometry()
	m.week.first = g.first
	if g.body <= 0 {
		return
	}
	slot, rows := m.weekSlot(), agenda.Rows(m.weekSlot())
	r0, r1 := m.week.at/slot, m.week.at/slot+1
	if _, block, ok := m.weekItem(m.week.day, m.week.pos); ok && block >= 0 {
		b := m.week.grids[m.week.day].Blocks[block]
		r0, r1 = b.Row0, b.Row1
	}
	top := m.week.top
	switch {
	case r1-r0 > g.body || r0 < top:
		top = r0
	case r1 > top+g.body:
		top = r1 - g.body
	}
	m.week.top = max(min(top, rows-g.body), 0)
}

func (m Model) handleWeekKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	g := m.weekGeometry()
	half, page := max(g.body/2, 1), max(g.body-1, 1)
	scroll := func(rows int) (tea.Model, tea.Cmd) {
		m.week.top = max(min(m.week.top+rows, agenda.Rows(m.weekSlot())-g.body), 0)
		return m, nil
	}
	switch s := msg.String(); {
	case s == "ctrl+d":
		return scroll(half)
	case s == "ctrl+u":
		return scroll(-half)
	case s == "pgdown":
		return scroll(page)
	case s == "pgup":
		return scroll(-page)
	case s == "+" || s == "=":
		return m.weekZoom(+1)
	case s == "-" || s == "_":
		return m.weekZoom(-1)
	case s == "D":
		m.week.five = !m.week.five
		if m.weekHidden(m.weekDate()) {
			m.week.day = m.weekNearestVisible(m.week.day)
			m.week.pos = m.weekPick(m.week.day, m.week.at)
		}
		m.weekReveal()
		return m, nil
	case key.Matches(msg, k.Right):
		return m.weekMoveDay(+1)
	case key.Matches(msg, k.Left):
		return m.weekMoveDay(-1)
	case key.Matches(msg, k.Down):
		return m.weekStep(+1), nil
	case key.Matches(msg, k.Up):
		return m.weekStep(-1), nil
	case key.Matches(msg, k.Top):
		if m.weekEntries(m.week.day) > 0 {
			m.weekSetPos(0)
		}
		m.weekReveal()
		return m, nil
	case key.Matches(msg, k.Bottom):
		if n := m.weekEntries(m.week.day); n > 0 {
			m.weekSetPos(n - 1)
		}
		m.weekReveal()
		return m, nil
	case key.Matches(msg, k.NextDay): // ]: next week, same weekday
		return m.showWeek(m.weekDate().AddDays(7), "", false)
	case key.Matches(msg, k.PrevDay):
		return m.showWeek(m.weekDate().AddDays(-7), "", false)
	case key.Matches(msg, k.Open):
		if it, ok := m.selectedWeekItem(); ok {
			return m, m.openDetail(it)
		}
	}
	return m, nil
}

// weekStep moves to the next or previous entry of the day; on a day with
// nothing it moves the selected time by a row.
func (m Model) weekStep(dir int) Model {
	n := m.weekEntries(m.week.day)
	if n == 0 {
		slot := m.weekSlot()
		m.week.at = max(min(m.week.at+dir*slot, 1440-1), 0)
		m.week.pos = -1
		m.weekReveal()
		return m
	}
	m.weekSetPos(max(min(m.week.pos+dir, n-1), 0))
	m.weekReveal()
	return m
}

// weekMoveDay moves to the next or previous shown day, keeping the time and so
// landing on what is there. Past the edge it goes to the next or previous week.
func (m Model) weekMoveDay(dir int) (tea.Model, tea.Cmd) {
	vis := m.weekVisible()
	for i, d := range vis {
		if d != m.week.day {
			continue
		}
		if j := i + dir; j >= 0 && j < len(vis) {
			m.week.day = vis[j]
			m.week.pos = m.weekPick(m.week.day, m.week.at)
			m.weekReveal()
			return m, nil
		}
	}
	date := m.weekDate().AddDays(dir)
	for m.weekHidden(date) {
		date = date.AddDays(dir)
	}
	return m.showWeek(date, "", false)
}

// weekZoom changes the minutes a row to the next finer (dir > 0) or coarser
// step, keeping the selected time where it is on the screen.
func (m Model) weekZoom(dir int) (tea.Model, tea.Cmd) {
	cur := 1
	for i, s := range weekSlots {
		if s == m.weekSlot() {
			cur = i
		}
	}
	next := max(min(cur+dir, len(weekSlots)-1), 0)
	if next == cur {
		return m, nil
	}
	offset := m.week.at/m.weekSlot() - m.week.top
	it, had := m.selectedWeekItem()
	m.week.slot = weekSlots[next]
	m.rebuildWeek()
	if had { // the order of a crowded hour can change with the zoom
		for i := 0; i < m.weekEntries(m.week.day); i++ {
			if o, _, _ := m.weekItem(m.week.day, i); o.Event.ID == it.Event.ID && o.Start.Equal(it.Start) {
				m.week.pos = i
				break
			}
		}
	}
	m.week.top = max(m.week.at/m.week.slot-offset, 0)
	m.weekReveal()
	return m, nil
}
