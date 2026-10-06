package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// The focus view (key 4): the completion heatmap and, as it grows, the
// panels around it. This file is its part of the model: loading the archive
// and the heatmap's keys.

type heatLoadedMsg struct {
	days map[domain.CivilDate][]write.Archived
	err  error
}

// heatEnabled reports whether the view exists: it reads the Write archive, so
// it goes when Write is off.
func (m Model) heatEnabled() bool { return m.writeOn }

// loadHeat reads the archive and groups it by completion day.
func (m Model) loadHeat() tea.Cmd {
	w := m.writes
	if w == nil || !m.heatEnabled() {
		return nil
	}
	return func() tea.Msg {
		list, err := w.WriteArchived()
		return heatLoadedMsg{days: write.CompletionsByDay(list), err: err}
	}
}

// reloadHeat rereads the archive while the heatmap is on screen, since
// `clical write` or another editor may have changed it.
func (m Model) reloadHeat() tea.Cmd {
	if m.view != viewHeat {
		return nil
	}
	return m.loadHeat()
}

func (m Model) heatLoaded(msg heatLoadedMsg) Model {
	if msg.err != nil {
		m.heat.err = msg.err
		return m
	}
	m.heat = m.heat.withData(msg.days, m.now)
	today := m.today()
	m.heat.cursor = m.heat.clamp(m.heat.cursor, today)
	m.heat.anchor = m.heat.clamp(m.heat.anchor, today)
	return m.heatFit()
}

// showHeat switches to the focus view with day under the cursor.
func (m Model) showHeat(day domain.CivilDate) (tea.Model, tea.Cmd) {
	m.view = viewHeat
	m.focus = focusMain
	m.heat = m.heat.goTo(day, m.today())
	return m.heatFit(), m.loadHeat()
}

// heatFit scrolls the heatmap so the cursor is in view.
func (m Model) heatFit() Model {
	_, hi := m.heat.bounds(m.today())
	m.heat.left = m.heat.viewLeft(m.heatCellsWidth(), m.weekStart, hi)
	return m
}

// mainWidth is the width of the main panel: what's left between the calendar
// list and the global Write panel.
func (m Model) mainWidth() int {
	side := sidebarInner + 2
	if m.sidebarCollapsed {
		side = collapsedSidebarInner + 2
	}
	gw, _ := m.globalRect()
	return m.width - side - gw
}

// heatCellsWidth is the room for the heatmap's day columns.
func (m Model) heatCellsWidth() int {
	return m.mainWidth() - 2 - heatLabelW
}

// handleHeatKey moves the heatmap's cursor. It reports false for keys that
// aren't the heatmap's, which then work as everywhere.
func (m Model) handleHeatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	var dir heatDir
	extend := false
	switch msg.String() {
	case "j", "down":
		dir = heatDown
	case "k", "up":
		dir = heatUp
	case "h", "left":
		dir = heatLeft
	case "l", "right":
		dir = heatRight
	case "J", "shift+down":
		dir, extend = heatDown, true
	case "K", "shift+up":
		dir, extend = heatUp, true
	case "H", "shift+left":
		dir, extend = heatLeft, true
	case "L", "shift+right":
		dir, extend = heatRight, true
	case "space":
		m.heat = m.heat.cycleUnit()
		return m.heatFit(), nil, true
	case "t":
		m.heat = m.heat.goTo(m.today(), m.today())
		return m.heatFit(), nil, true
	case "esc":
		h, ok := m.heat.collapse()
		m.heat = h
		return m.heatFit(), nil, ok
	default:
		return m, nil, false
	}
	m.heat = m.heat.move(dir, extend, m.today())
	return m.heatFit(), nil, true
}
