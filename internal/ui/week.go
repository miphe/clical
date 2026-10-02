package ui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// weekFirstDay is the first column of the week view. Configurable in M9.
const weekFirstDay = time.Monday

// weekState is the week view's part of the model: one week of events, laid
// out as seven days, and a selection of (column, event in that column).
type weekState struct {
	start   domain.CivilDate
	loaded  bool // events hold start's week
	loading bool
	events  []domain.Event
	days    []agenda.Day // derived: visible events grouped by day

	day  int // selected column, 0–6
	item int // selected event within that column

	// When pendingSelect is set, the next load selects pendingID in the
	// selected column, or that column's default event.
	pendingSelect bool
	pendingID     string
}

func (m Model) loadWeek() tea.Cmd {
	src, loc, start := m.src, m.loc, m.week.start
	return func() tea.Msg {
		events, err := src.Events(context.Background(), start.In(loc), start.AddDays(7).In(loc))
		return weekLoadedMsg{start: start, events: events, err: err}
	}
}

// showWeek switches to the week containing day, selects day's column and,
// if found there, eventID.
func (m Model) showWeek(day domain.CivilDate, eventID string) (tea.Model, tea.Cmd) {
	m.view = viewWeek
	m.focus = focusMain
	start := agenda.WeekStart(day, weekFirstDay)
	m.week.day = daysBetween(start, day)
	m.week.pendingSelect, m.week.pendingID = true, eventID

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

// rebuildWeek regroups the week's visible events into days.
func (m *Model) rebuildWeek() {
	if !m.week.loaded {
		return
	}
	var visible []domain.Event
	for _, e := range m.week.events {
		if !m.hidden[e.CalendarID] {
			visible = append(visible, e)
		}
	}
	m.week.days = agenda.Build(visible, m.week.start, 7, m.loc)
	m.week.item = min(m.week.item, max(0, len(m.weekItems())-1))
}

// applyWeekSelection resolves a pending selection once the week is loaded.
func (m *Model) applyWeekSelection() {
	if !m.week.pendingSelect || !m.week.loaded {
		return
	}
	m.week.pendingSelect = false
	items := m.weekItems()
	for i, it := range items {
		if m.week.pendingID != "" && it.Event.ID == m.week.pendingID {
			m.week.item = i
			return
		}
	}
	m.week.item = m.defaultWeekItem()
}

// defaultWeekItem is today's first event after now, or a day's first event.
func (m Model) defaultWeekItem() int {
	if m.selectedWeekDate() != m.today() {
		return 0
	}
	items := m.weekItems()
	for i, it := range items {
		if !it.Event.AllDay && it.Start.After(m.now) {
			return i
		}
	}
	return max(0, len(items)-1)
}

func (m Model) weekItems() []agenda.Item {
	if m.week.day < 0 || m.week.day >= len(m.week.days) {
		return nil
	}
	return m.week.days[m.week.day].Items
}

func (m Model) selectedWeekDate() domain.CivilDate {
	return m.week.start.AddDays(m.week.day)
}

func (m Model) selectedWeekItem() (agenda.Item, bool) {
	items := m.weekItems()
	if m.week.item < 0 || m.week.item >= len(items) {
		return agenda.Item{}, false
	}
	return items[m.week.item], true
}

func (m Model) selectedWeekEventID() string {
	if it, ok := m.selectedWeekItem(); ok {
		return it.Event.ID
	}
	return ""
}

func (m Model) handleWeekKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.FocusMain): // l / right: next column
		if m.week.day < 6 {
			m.week.day++
			m.week.item = m.defaultWeekItem()
		}
	case key.Matches(msg, k.FocusSidebar): // h / left: previous column, then the sidebar
		if m.week.day > 0 {
			m.week.day--
			m.week.item = m.defaultWeekItem()
		} else {
			m.focus = focusSidebar
		}
	case key.Matches(msg, k.Down):
		m.week.item = min(m.week.item+1, max(0, len(m.weekItems())-1))
	case key.Matches(msg, k.Up):
		m.week.item = max(m.week.item-1, 0)
	case key.Matches(msg, k.Top):
		m.week.item = 0
	case key.Matches(msg, k.Bottom):
		m.week.item = max(0, len(m.weekItems())-1)
	case key.Matches(msg, k.NextDay): // ]: next week, same weekday
		return m.showWeek(m.selectedWeekDate().AddDays(7), "")
	case key.Matches(msg, k.PrevDay):
		return m.showWeek(m.selectedWeekDate().AddDays(-7), "")
	case key.Matches(msg, k.Open):
		if it, ok := m.selectedWeekItem(); ok {
			m.openDetail(it)
		}
	}
	return m, nil
}

func daysBetween(from, to domain.CivilDate) int {
	return int(to.In(time.UTC).Sub(from.In(time.UTC)).Hours() / 24)
}
