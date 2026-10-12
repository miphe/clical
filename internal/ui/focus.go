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
	m.heat = m.heat.goTo(day, m.today())
	m = m.heatFit()
	// The agenda beside it answers "what's next", so it starts at today.
	next, cmd := m.jumpTo(m.today())
	m = next.(Model)
	m.focus = focusHeat
	return m.fitFocusView(), tea.Batch(cmd, m.loadHeat())
}

// fitFocusView keeps the focus view's parts in order as the window or the
// view changes: the calendar list is collapsed when the panels would be
// squeezed (and given back when there is room, or in another view), a panel
// that was dropped doesn't keep the focus, and the agenda stays in view.
func (m Model) fitFocusView() Model {
	if m.width == 0 {
		return m
	}
	open := m
	open.sidebarCollapsed = false
	roomy := m.view != viewHeat || open.mainWidth() >= agendaPanelW+minTasksW
	switch {
	case !roomy && !m.sidebarCollapsed:
		m.sidebarCollapsed, m.sideAuto = true, true
	case roomy && m.sideAuto:
		m.sidebarCollapsed, m.sideAuto = false, false
	}
	if m.view != viewHeat {
		return m
	}
	lay := m.focusLayout()
	if (m.focus == focusMain && lay.agendaH == 0) || (m.focus == focusTasks && lay.tasksH == 0) {
		m.focus = focusHeat
	}
	m.ensureVisible()
	return m
}

// heatFit scrolls the heatmap so the cursor is in view.
func (m Model) heatFit() Model {
	_, hi := m.heat.bounds(m.today())
	w := m.heatCellsWidth()
	m.heat.left = m.heat.viewLeft(w, hi)
	// Showing the newest column? Then keep doing so when a new day (or week,
	// or month) begins while the app is open.
	cols := heatColumns(m.heat.left, hi, w)
	m.heat.pinned = len(cols) > 0 && cols[len(cols)-1].start == colStart(hi)
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

// mainFocus is the panel focus returns to after a side panel: the view's main
// one.
func (m Model) mainFocus() focus {
	if m.view == viewHeat {
		return focusHeat
	}
	return focusMain
}

// handleTasksKey scrolls the completed-tasks list while it has focus.
func (m Model) handleTasksKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	_, room := m.tasksView()
	page := max(room-1, 1)
	switch msg.String() {
	case "j", "down":
		return m.scrollTasks(1), nil, true
	case "k", "up":
		return m.scrollTasks(-1), nil, true
	case "pgdown", "space":
		return m.scrollTasks(page), nil, true
	case "pgup":
		return m.scrollTasks(-page), nil, true
	case "g", "home":
		m.heat.scroll = 0
		return m, nil, true
	case "G", "end":
		return m.scrollTasks(1 << 20), nil, true
	case "esc":
		m.focus = focusHeat
		return m, nil, true
	}
	return m, nil, false
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
		m.heat.scroll = 0
		return m.heatFit(), nil, true
	case "t":
		m.heat = m.heat.goTo(m.today(), m.today())
		m.heat.scroll = 0
		return m.heatFit(), nil, true
	case "esc":
		h, ok := m.heat.collapse()
		m.heat = h
		m.heat.scroll = 0
		return m.heatFit(), nil, ok
	case "pgdown":
		_, room := m.tasksView()
		return m.scrollTasks(max(room-1, 1)), nil, true
	case "pgup":
		_, room := m.tasksView()
		return m.scrollTasks(-max(room-1, 1)), nil, true
	default:
		return m, nil, false
	}
	m.heat = m.heat.move(dir, extend, m.today())
	m.heat.scroll = 0
	return m.heatFit(), nil, true
}
