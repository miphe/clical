package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Focus+ (key 5): the focus view reworked around a Today panel that can be
// left open all day. It is tried beside the focus view on 4, which is
// unchanged, until it is judged the better one (as Week+ was, M17).
//
// One column: Today on top, the completed tasks under it, the heatmap along
// the bottom. The part that has focus gets the room: with Today (or the
// list, or Write) focused, Today is large and the completed tasks show their
// newest lines; with the heatmap or the completed tasks focused, those grow
// and Today keeps only its now/next lines and its strip. It shares its
// state with 4: the agenda's rows and selection, the heatmap and the
// archive.

const (
	plusTasksMaxH = 7  // the completed tasks, compact: the newest five lines and a border
	plusTasksMinH = 4  // or the newest two, when the window is short
	plusMinListH  = 3  // below this Today's list isn't worth the room
	plusMainMinW  = 52 // what the main area keeps beside Write: the view tabs
	plusWriteMinW = 32
	plusWriteMaxW = 48 // while the day needs the room
	// Once the main area can keep plusMainRoomyW, Write takes what's beyond
	// it, up to plusWriteWideW: on a wide screen its items get to be read.
	plusMainRoomyW = 80
	plusWriteWideW = 2 * plusWriteMaxW
)

// isFocusView reports whether a focus view is on screen, 4 or 5: what they
// share (the archive, the heatmap's and the tasks' keys) applies to both.
func (m Model) isFocusView() bool { return m.view == viewHeat || m.view == viewFocusPlus }

// plusLayout is where Focus+'s panels go, as heights in the main column.
type plusLayout struct {
	todayH, tasksH, heatH int
	todayCompact          bool // the now/next lines and the strip, no list
	history               bool // the heatmap or the completed tasks lead
}

func (m Model) focusPlusLayout() plusLayout {
	h := m.bodyHeight()
	lay := plusLayout{history: m.focus == focusHeat || m.focus == focusTasks}
	compactH := 2 + m.todayTopH(m.todayContentW()) - 1 // no blank row under it
	lay.heatH = heatPanelH
	rest := h - lay.heatH
	if rest < 4 { // not even one now/next line: the heatmap alone
		lay.heatH = h
		return lay
	}
	if lay.history {
		lay.todayH, lay.todayCompact = min(compactH, rest), true
		lay.tasksH = rest - lay.todayH
		if lay.tasksH < minTasksPanel {
			lay.todayH, lay.tasksH = rest, 0
		}
		return lay
	}
	lay.tasksH = min(max(rest/4, plusTasksMinH), plusTasksMaxH)
	lay.todayH = rest - lay.tasksH
	if lay.todayH < compactH+plusMinListH {
		lay.todayH, lay.tasksH = rest, 0
	}
	lay.todayCompact = lay.todayH < compactH+plusMinListH
	return lay
}

// todayContentW is the width of the Today panel's content: inside its border
// and a column of padding.
func (m Model) todayContentW() int { return m.mainWidth() - 3 }

// plusWriteWidth is the global Write panel's width in Focus+: about a third
// of what's beside the calendar list, so the day keeps most of the room, and
// a strip when the main area would drop below what its tabs need. When the
// window is wide enough that the day has more than it needs, Write gets the
// rest, up to twice its usual most.
func (m Model) plusWriteWidth(avail int) int {
	if !m.globalOpen {
		return stripW
	}
	w := min(max(avail/3, plusWriteMinW), plusWriteMaxW)
	w = min(w, avail-plusMainMinW)
	if w < plusWriteMinW-4 {
		return stripW
	}
	return max(w, min(avail-plusMainRoomyW, plusWriteWideW))
}

// showFocusPlus switches to Focus+ with Today focused and the agenda at
// today. The calendar list is collapsed, and given back in another view.
func (m Model) showFocusPlus() (tea.Model, tea.Cmd) {
	from := m.view
	m.view = viewFocusPlus
	if from != viewHeat {
		m.heat = m.heat.goTo(m.today(), m.today())
	}
	m = m.heatSelected() // the newest completed tasks, from 4 too
	if !m.sidebarCollapsed {
		m.sidebarCollapsed, m.sideAuto = true, true
	}
	m = m.heatFit()
	next, cmd := m.jumpTo(m.today())
	m = next.(Model)
	m.focus = focusMain
	return m.fitFocusPlus(), tea.Batch(cmd, m.loadHeat())
}

// fitFocusPlus keeps the focus on a panel that is shown and the selection in
// view as the window changes.
func (m Model) fitFocusPlus() Model {
	lay := m.focusPlusLayout()
	if (m.focus == focusMain && lay.todayH == 0) || (m.focus == focusTasks && lay.tasksH == 0) {
		m.focus = focusHeat
	}
	m.ensureVisible()
	return m
}

// renderFocusPlus draws Focus+ in a width×height area. The view tabs sit in
// Today's top edge, or the heatmap's when Today has no room.
func (m Model) renderFocusPlus(width, height int, tabs []tab) []string {
	lay := m.focusPlusLayout()
	var out []string
	if lay.todayH > 0 {
		out = append(out, m.renderTodayPanel(width, lay.todayH, tabs, lay.todayCompact)...)
	}
	if lay.tasksH > 0 {
		completed := []tab{{label: "Completed", active: true}}
		if lay.history {
			out = append(out, m.renderTasks(width, lay.tasksH, completed)...)
		} else {
			out = append(out, m.renderTasksTail(width, lay.tasksH, completed)...)
		}
	}
	heatTabs := []tab{{label: "Heatmap", active: true}}
	if lay.todayH == 0 {
		heatTabs = tabs
	}
	heat := m.renderHeat(width-2, lay.heatH-2)
	out = append(out, panel(width, lay.heatH, frame{focused: m.focus == focusHeat, tabs: heatTabs, legend: heatLegend()}, heat)...)
	return padLines(out, height, width)
}

// renderTodayPanel draws the Today panel: now and next, the strip, and
// (unless compact) the agenda's rows with their titles in full.
func (m Model) renderTodayPanel(width, height int, tabs []tab, compact bool) []string {
	cw := width - 3
	var content []string
	content = append(content, m.todayHeaderFit(cw, height-2)...)
	// The strip goes whole or not at all: its labels alone say nothing.
	if cw >= todayMinStrip && len(content)+todayStripH <= height-2 {
		content = append(content, m.todayStrip(cw)...)
	}
	if !compact {
		content = append(content, "")
		room := height - 2 - len(content)
		if room > 0 {
			var list []string
			switch {
			case len(m.rows) == 0:
				list = m.renderAgenda(cw, 1)
			default:
				lines, _ := m.todayLines(min(m.offset, len(m.rows)-1), cw)
				list = lines[:min(room, len(lines))]
			}
			content = append(content, list...)
		}
	}
	for i := range content {
		content[i] = " " + content[i]
	}
	right := m.today().In(m.loc).Format("Mon 2 Jan")
	return panel(width, height, frame{focused: m.focus == focusMain, tabs: tabs, right: right}, content)
}

// renderTasksTail is the completed tasks when another panel leads: the newest
// lines of the selected days, the ones at the end.
func (m Model) renderTasksTail(width, height int, tabs []tab) []string {
	inner, room := width-2, height-2
	lo, hi := m.heat.span(m.weekStart)
	var content []string
	lines, msg := m.heatTaskLines(inner)
	if msg != "" {
		content = []string{mutedStyle.Render(msg)}
	} else {
		content = lines[max(len(lines)-room, 0):]
	}
	right := m.heatTitle(lo, hi)
	if tasks, notes, _ := m.heatTally(lo, hi); tasks+notes > 0 && m.heat.loaded {
		right += " · " + itemCount(tasks, notes)
	}
	return panel(width, height, frame{focused: m.focus == focusTasks, tabs: tabs, right: right}, content)
}

// itemCount is "2 tasks", "1 note", "3 tasks, 1 note".
func itemCount(tasks, notes int) string {
	var parts []string
	if tasks > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", tasks, plural(tasks, "task", "tasks")))
	}
	if notes > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", notes, plural(notes, "note", "notes")))
	}
	return strings.Join(parts, ", ")
}
