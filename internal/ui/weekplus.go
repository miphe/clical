package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// Week+ is the week as a time grid: a column per day and the time of day
// down the side (see weekplusview.go for how it is drawn). This file is its
// state, loading and keys. It reads events the way the Week view does, one
// week at a time, and keeps its own copy: the two views don't share state, so
// either can go without the other.
//
// What the user has selected is a time of day (at) and an entry of the day:
// the day's all-day items, then its timed blocks in order. Moving with j/k
// goes from entry to entry, but the time is what stays when changing day: h
// and l land on whatever is at that time. On a day with nothing, the time
// alone is the selection, and j/k move it, which is also where choosing a
// free slot would start.

const (
	wpMinDayCol = 14 // the narrowest day column that still gets a neighbour
	wpMaxLanes  = 3  // events side by side at most; the rest are counted
	wpMinLane   = 6  // the narrowest lane worth drawing
	wpBandMax   = 3  // rows of all-day events above the grid
	wpDefaultAt = 8  // the hour shown first when nothing says otherwise
)

// wpSlots are the zoom steps, in minutes a row, coarsest first.
var wpSlots = []int{60, 30, 15}

type weekPlusState struct {
	start   domain.CivilDate
	loaded  bool // events hold start's week
	loading bool
	events  []domain.Event
	grids   []agenda.DayGrid // derived: the visible events, a grid for each day

	day  int  // selected day, 0–6 from start
	pos  int  // selected entry of the day (see wpEntries), -1 for none
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

type weekPlusLoadedMsg struct {
	start  domain.CivilDate
	events []domain.Event
	err    error
}

func (m Model) wpSlot() int {
	if m.wp.slot == 0 {
		return 30
	}
	return m.wp.slot
}

func (m Model) loadWeekPlus() tea.Cmd {
	src, loc, start := m.src, m.loc, m.wp.start
	return func() tea.Msg {
		events, err := src.Events(context.Background(), start.In(loc), start.AddDays(7).In(loc))
		return weekPlusLoadedMsg{start: start, events: events, err: err}
	}
}

// wpHidden reports whether the weekday of d is left out of a weekdays-only week.
func (m Model) wpHidden(d domain.CivilDate) bool {
	wd := d.Weekday()
	return m.wp.five && (wd == time.Saturday || wd == time.Sunday)
}

// wpVisible is the days of the week shown, as indexes from the first.
func (m Model) wpVisible() []int {
	var vis []int
	for i := 0; i < 7; i++ {
		if !m.wpHidden(m.wp.start.AddDays(i)) {
			vis = append(vis, i)
		}
	}
	return vis
}

func (m Model) wpDate() domain.CivilDate { return m.wp.start.AddDays(m.wp.day) }

// showWeekPlus switches to the week containing day and selects day and, if
// found there, eventID. fit scrolls to the time now, or to the day's events.
func (m Model) showWeekPlus(day domain.CivilDate, eventID string, fit bool) (tea.Model, tea.Cmd) {
	m.view = viewWeekPlus
	m.focus = focusMain
	start := agenda.WeekStart(day, m.weekStart)
	m.wp.day = daysBetween(start, day)
	m.wp.pendingSelect, m.wp.pendingID = true, eventID
	m.wp.pendingNow = fit && eventID == "" && day == m.today()
	m.wp.fit = m.wp.fit || fit

	if m.wp.loaded && start == m.wp.start {
		m.applyWeekPlusSelection()
		return m, nil
	}
	m.wp.start = start
	m.wp.loaded = false
	m.wp.loading = true
	return m, m.loadWeekPlus()
}

func (m Model) weekPlusLoaded(msg weekPlusLoadedMsg) Model {
	if msg.start != m.wp.start {
		return m // a newer load is in flight
	}
	m.wp.loading = false
	m.loadErr = msg.err
	if msg.err != nil {
		return m
	}
	m.wp.events = msg.events
	m.wp.loaded = true
	m.rebuildWeekPlus()
	m.applyWeekPlusSelection()
	return m
}

// rebuildWeekPlus lays the week's visible events out again.
func (m *Model) rebuildWeekPlus() {
	if !m.wp.loaded {
		return
	}
	var visible []domain.Event
	for _, e := range m.wp.events {
		if m.shows(e) {
			visible = append(visible, e)
		}
	}
	days := agenda.Build(visible, m.wp.start, 7, m.loc)
	m.wp.grids = make([]agenda.DayGrid, len(days))
	for i, d := range days {
		m.wp.grids[i] = agenda.BuildGrid(d, m.loc, m.wpSlot())
	}
	m.wp.pos = min(m.wp.pos, m.wpEntries(m.wp.day)-1)
}

// wpEntries is how many things can be selected on day d: its all-day items,
// then its timed blocks.
func (m Model) wpEntries(d int) int {
	if d < 0 || d >= len(m.wp.grids) {
		return 0
	}
	g := m.wp.grids[d]
	return len(g.AllDay) + len(g.Blocks)
}

// wpItem is the item at entry pos of day d, and its block (-1 for an
// all-day item).
func (m Model) wpItem(d, pos int) (it agenda.Item, block int, ok bool) {
	if d < 0 || d >= len(m.wp.grids) || pos < 0 || pos >= m.wpEntries(d) {
		return agenda.Item{}, -1, false
	}
	g := m.wp.grids[d]
	if pos < len(g.AllDay) {
		return g.AllDay[pos], -1, true
	}
	return g.Blocks[pos-len(g.AllDay)].Item, pos - len(g.AllDay), true
}

func (m Model) selectedWeekPlusItem() (agenda.Item, bool) {
	it, _, ok := m.wpItem(m.wp.day, m.wp.pos)
	return it, ok
}

func (m Model) selectedWeekPlusEventID() string {
	if it, ok := m.selectedWeekPlusItem(); ok {
		return it.Event.ID
	}
	return ""
}

// wpPick is the entry of day d that is at time at: the block covering it,
// else the block starting nearest, else the first all-day item, else none.
func (m Model) wpPick(d, at int) int {
	if d < 0 || d >= len(m.wp.grids) {
		return -1
	}
	g := m.wp.grids[d]
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

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// wpSetPos selects an entry of the selected day and takes its time.
func (m *Model) wpSetPos(pos int) {
	m.wp.pos = pos
	if _, block, ok := m.wpItem(m.wp.day, pos); ok && block >= 0 {
		m.wp.at = m.wp.grids[m.wp.day].Blocks[block].Start
	}
}

// applyWeekPlusSelection resolves a pending selection once the week is loaded.
func (m *Model) applyWeekPlusSelection() {
	if !m.wp.pendingSelect || !m.wp.loaded {
		return
	}
	m.wp.pendingSelect = false
	if m.wpHidden(m.wpDate()) {
		m.wp.day = m.wpNearestVisible(m.wp.day)
	}
	if m.wp.pendingNow {
		m.wp.at = agenda.MinutesOfDay(m.now.In(m.loc))
	}
	m.wp.pos = -1
	if id := m.wp.pendingID; id != "" {
		for i := 0; i < m.wpEntries(m.wp.day); i++ {
			if it, _, _ := m.wpItem(m.wp.day, i); it.Event.ID == id {
				m.wpSetPos(i)
				break
			}
		}
	}
	if m.wp.pos < 0 {
		m.wp.pos = m.wpPick(m.wp.day, m.wp.at)
	}
	if m.wp.fit {
		m.wp.fit = false
		m.wpFit()
	}
	m.wpReveal()
}

// wpNearestVisible is the visible day nearest to day index d, the later one
// when they are equally near.
func (m Model) wpNearestVisible(d int) int {
	best, gap := d, 1<<30
	for _, v := range m.wpVisible() {
		if g := abs(v - d); g < gap || (g == gap && v > best) {
			best, gap = v, g
		}
	}
	return best
}

// wpFit scrolls to where the week's day starts: around the time now when the
// week is this one, else just above the earliest event of the days shown, else
// the usual start of a working day.
func (m *Model) wpFit() {
	g := m.wpGeometry()
	slot, rows := m.wpSlot(), agenda.Rows(m.wpSlot())
	top := wpDefaultAt * 60 / slot
	if today := daysBetween(m.wp.start, m.today()); today >= 0 && today < 7 {
		top = agenda.MinutesOfDay(m.now.In(m.loc))/slot - g.body/3
	} else {
		first, found := rows, false
		for _, d := range m.wpVisible() {
			if d < len(m.wp.grids) && len(m.wp.grids[d].Blocks) > 0 {
				first, found = min(first, m.wp.grids[d].Blocks[0].Row0), true
			}
		}
		if found {
			top = first - 1
		}
	}
	m.wp.top = max(min(top, rows-g.body), 0)
}

// wpReveal scrolls, and slides the day columns, to keep the selection in view.
func (m *Model) wpReveal() {
	g := m.wpGeometry()
	m.wp.first = g.first
	if g.body <= 0 {
		return
	}
	slot, rows := m.wpSlot(), agenda.Rows(m.wpSlot())
	r0, r1 := m.wp.at/slot, m.wp.at/slot+1
	if _, block, ok := m.wpItem(m.wp.day, m.wp.pos); ok && block >= 0 {
		b := m.wp.grids[m.wp.day].Blocks[block]
		r0, r1 = b.Row0, b.Row1
	}
	top := m.wp.top
	switch {
	case r1-r0 > g.body || r0 < top:
		top = r0
	case r1 > top+g.body:
		top = r1 - g.body
	}
	m.wp.top = max(min(top, rows-g.body), 0)
}

func (m Model) handleWeekPlusKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	g := m.wpGeometry()
	half, page := max(g.body/2, 1), max(g.body-1, 1)
	scroll := func(rows int) (tea.Model, tea.Cmd) {
		m.wp.top = max(min(m.wp.top+rows, agenda.Rows(m.wpSlot())-g.body), 0)
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
		return m.wpZoom(+1)
	case s == "-" || s == "_":
		return m.wpZoom(-1)
	case s == "D":
		m.wp.five = !m.wp.five
		if m.wpHidden(m.wpDate()) {
			m.wp.day = m.wpNearestVisible(m.wp.day)
			m.wp.pos = m.wpPick(m.wp.day, m.wp.at)
		}
		m.wpReveal()
		return m, nil
	case key.Matches(msg, k.Right):
		return m.wpMoveDay(+1)
	case key.Matches(msg, k.Left):
		return m.wpMoveDay(-1)
	case key.Matches(msg, k.Down):
		return m.wpStep(+1), nil
	case key.Matches(msg, k.Up):
		return m.wpStep(-1), nil
	case key.Matches(msg, k.Top):
		if m.wpEntries(m.wp.day) > 0 {
			m.wpSetPos(0)
		}
		m.wpReveal()
		return m, nil
	case key.Matches(msg, k.Bottom):
		if n := m.wpEntries(m.wp.day); n > 0 {
			m.wpSetPos(n - 1)
		}
		m.wpReveal()
		return m, nil
	case key.Matches(msg, k.NextDay): // ]: next week, same weekday
		return m.showWeekPlus(m.wpDate().AddDays(7), "", false)
	case key.Matches(msg, k.PrevDay):
		return m.showWeekPlus(m.wpDate().AddDays(-7), "", false)
	case key.Matches(msg, k.Open):
		if it, ok := m.selectedWeekPlusItem(); ok {
			return m, m.openDetail(it)
		}
	}
	return m, nil
}

// wpStep moves to the next or previous entry of the day; on a day with
// nothing it moves the selected time by a row.
func (m Model) wpStep(dir int) Model {
	n := m.wpEntries(m.wp.day)
	if n == 0 {
		slot := m.wpSlot()
		m.wp.at = max(min(m.wp.at+dir*slot, 1440-1), 0)
		m.wp.pos = -1
		m.wpReveal()
		return m
	}
	m.wpSetPos(max(min(m.wp.pos+dir, n-1), 0))
	m.wpReveal()
	return m
}

// wpMoveDay moves to the next or previous shown day, keeping the time and so
// landing on what is there. Past the edge it goes to the next or previous week.
func (m Model) wpMoveDay(dir int) (tea.Model, tea.Cmd) {
	vis := m.wpVisible()
	for i, d := range vis {
		if d != m.wp.day {
			continue
		}
		if j := i + dir; j >= 0 && j < len(vis) {
			m.wp.day = vis[j]
			m.wp.pos = m.wpPick(m.wp.day, m.wp.at)
			m.wpReveal()
			return m, nil
		}
	}
	date := m.wpDate().AddDays(dir)
	for m.wpHidden(date) {
		date = date.AddDays(dir)
	}
	return m.showWeekPlus(date, "", false)
}

// wpZoom changes the minutes a row to the next finer (dir > 0) or coarser
// step, keeping the selected time where it is on the screen.
func (m Model) wpZoom(dir int) (tea.Model, tea.Cmd) {
	cur := 1
	for i, s := range wpSlots {
		if s == m.wpSlot() {
			cur = i
		}
	}
	next := max(min(cur+dir, len(wpSlots)-1), 0)
	if next == cur {
		return m, nil
	}
	offset := m.wp.at/m.wpSlot() - m.wp.top
	it, had := m.selectedWeekPlusItem()
	m.wp.slot = wpSlots[next]
	m.rebuildWeekPlus()
	if had { // the order of a crowded hour can change with the zoom
		for i := 0; i < m.wpEntries(m.wp.day); i++ {
			if o, _, _ := m.wpItem(m.wp.day, i); o.Event.ID == it.Event.ID && o.Start.Equal(it.Start) {
				m.wp.pos = i
				break
			}
		}
	}
	m.wp.top = max(m.wp.at/m.wp.slot-offset, 0)
	m.wpReveal()
	return m, nil
}
