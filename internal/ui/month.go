package ui

import (
	"context"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/domain"
)

// monthState is the month view's part of the model: the shown month as a
// grid of whole weeks, and a selected day. The month view selects days, not
// events; enter opens the selected day in the agenda.
type monthState struct {
	month   domain.CivilDate // first day of the shown month
	start   domain.CivilDate // the grid's first day (may be in the previous month)
	weeks   int              // grid rows, 4–6
	loaded  bool
	loading bool
	events  []domain.Event
	days    []agenda.Day // derived: visible events grouped by grid day

	sel domain.CivilDate
}

func (m Model) loadMonth() tea.Cmd {
	src, loc, start, weeks := m.src, m.loc, m.month.start, m.month.weeks
	return func() tea.Msg {
		events, err := src.Events(context.Background(), start.In(loc), start.AddDays(weeks*7).In(loc))
		return monthLoadedMsg{start: start, weeks: weeks, events: events, err: err}
	}
}

// showMonth switches to the month containing day, with day selected; moving
// the selection out of the shown month loads the new month.
func (m Model) showMonth(day domain.CivilDate) (tea.Model, tea.Cmd) {
	m.view = viewMonth
	m.focus = focusMain
	m.month.sel = day
	first := domain.NewCivilDate(day.Year, day.Month, 1)
	if m.month.loaded && first == m.month.month {
		return m, nil
	}
	m.month.month = first
	m.month.start, m.month.weeks = agenda.MonthGrid(first, weekFirstDay)
	m.month.loaded = false
	m.month.loading = true
	return m, m.loadMonth()
}

func (m Model) monthLoaded(msg monthLoadedMsg) Model {
	if msg.start != m.month.start || msg.weeks != m.month.weeks {
		return m // a newer load is in flight
	}
	m.month.loading = false
	m.loadErr = msg.err
	if msg.err != nil {
		return m
	}
	m.month.events = msg.events
	m.month.loaded = true
	m.rebuildMonth()
	return m
}

// rebuildMonth regroups the month's visible events into grid days.
func (m *Model) rebuildMonth() {
	if !m.month.loaded {
		return
	}
	var visible []domain.Event
	for _, e := range m.month.events {
		if m.shows(e) {
			visible = append(visible, e)
		}
	}
	m.month.days = agenda.Build(visible, m.month.start, m.month.weeks*7, m.loc)
}

func (m Model) handleMonthKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k, sel := m.keys, m.month.sel
	switch {
	case key.Matches(msg, k.Right):
		return m.showMonth(sel.AddDays(1))
	case key.Matches(msg, k.Left):
		return m.showMonth(sel.AddDays(-1))
	case key.Matches(msg, k.Down):
		return m.showMonth(sel.AddDays(7))
	case key.Matches(msg, k.Up):
		return m.showMonth(sel.AddDays(-7))
	case key.Matches(msg, k.Top):
		return m.showMonth(domain.NewCivilDate(sel.Year, sel.Month, 1))
	case key.Matches(msg, k.Bottom):
		return m.showMonth(domain.NewCivilDate(sel.Year, sel.Month, sel.DaysInMonth()))
	case key.Matches(msg, k.NextDay): // ]: next month
		return m.showMonth(sel.AddMonths(1))
	case key.Matches(msg, k.PrevDay):
		return m.showMonth(sel.AddMonths(-1))
	case key.Matches(msg, k.Open): // enter: this day in the agenda
		return m.showAgenda(sel, "")
	}
	return m, nil
}
