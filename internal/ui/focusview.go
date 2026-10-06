package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
)

// The focus view's layout: the completed tasks of the selected days above,
// the heatmap in its own panel along the bottom.

const (
	heatPanelH    = heatRows + 2 // the heatmap and its border
	minTasksPanel = 5            // below this the tasks panel is dropped
)

type focusLayout struct{ tasksH, heatH int }

func (m Model) focusLayout() focusLayout {
	h := m.bodyHeight()
	if h-heatPanelH < minTasksPanel {
		return focusLayout{heatH: h}
	}
	return focusLayout{tasksH: h - heatPanelH, heatH: heatPanelH}
}

// renderFocus draws the focus view's panels in a width×height area, with the
// view tabs in the top panel's edge.
func (m Model) renderFocus(width, height int, tabs []tab) []string {
	lay := m.focusLayout()
	var out []string
	heatTabs := []tab{{label: "Heatmap", active: true}}
	if lay.tasksH > 0 {
		out = m.renderTasks(width, lay.tasksH, tabs)
	} else {
		heatTabs = tabs
	}
	heat := m.renderHeat(width-2, lay.heatH-2)
	out = append(out, panel(width, lay.heatH, frame{focused: m.focus == focusHeat, tabs: heatTabs}, heat)...)
	return padLines(out, height, width)
}

// tasksView is the room for the task lines: inside the border, and under the
// header and its blank line when the selection has one.
func (m Model) tasksView() (width, height int) {
	height = m.focusLayout().tasksH - 2
	if m.heatHasHeader() {
		height -= 2
	}
	return m.mainWidth() - 2, height
}

// heatHasHeader reports whether the selection is named above the list: only
// when it is more than one day, since a single day is named by its own label.
func (m Model) heatHasHeader() bool {
	lo, hi := m.heat.span(m.weekStart)
	return lo != hi
}

// renderTasks draws the completed-tasks panel.
func (m Model) renderTasks(width, height int, tabs []tab) []string {
	inner := width - 2
	lo, hi := m.heat.span(m.weekStart)
	var content []string
	room := height - 2
	if m.heatHasHeader() {
		content = []string{fit(m.heatHeader(lo, hi), inner), ""}
		room -= 2
	}
	var bottom string
	if lines, msg := m.heatTaskLines(inner); msg != "" {
		content = append(content, mutedStyle.Render(msg))
	} else {
		room = max(room, 1)
		start := min(max(m.heat.scroll, 0), max(len(lines)-room, 0))
		end := min(start+room, len(lines))
		content = append(content, lines[start:end]...)
		if len(lines) > room {
			bottom = fmt.Sprintf("%d–%d of %d", start+1, end, len(lines))
		}
	}
	return panel(width, height, frame{focused: m.focus == focusTasks, tabs: tabs, bottom: bottom}, content)
}

// heatHeader names a selection of several days and how much was done in it.
func (m Model) heatHeader(lo, hi domain.CivilDate) string {
	title := boldStyle.Render(m.heatTitle(lo, hi))
	if !m.heat.loaded || m.heat.err != nil || len(m.heat.days) == 0 {
		return title
	}
	tasks, busy := m.heatTally(lo, hi)
	switch {
	case tasks == 0:
		return title
	case busy > 1:
		return title + mutedStyle.Render(fmt.Sprintf("  ·  %d %s on %d days", tasks, plural(tasks, "task", "tasks"), busy))
	}
	return title + mutedStyle.Render(fmt.Sprintf("  ·  %d %s", tasks, plural(tasks, "task", "tasks")))
}

// heatTally counts the tasks completed from lo to hi, and the days they fell on.
func (m Model) heatTally(lo, hi domain.CivilDate) (tasks, busy int) {
	for d := lo; !hi.Before(d); d = d.AddDays(1) {
		if n := len(m.heat.days[d]); n > 0 {
			tasks += n
			busy++
		}
	}
	return tasks, busy
}

// heatTaskLines lays out the selected days' tasks, oldest first so the newest
// is last: a day's header, then its tasks. It returns a message instead when
// there is nothing to list.
func (m Model) heatTaskLines(width int) ([]string, string) {
	lo, hi := m.heat.span(m.weekStart)
	switch {
	case m.heat.err != nil:
		return nil, "Can't read the archive: " + m.heat.err.Error()
	case !m.heat.loaded:
		return nil, "Loading…"
	case len(m.heat.days) == 0:
		return nil, "No archived tasks yet. Check a TODO in a Write panel and press z to archive it."
	}
	// A time is "09:30" or "9:30 AM": give every row the longest one's room.
	timeW := 0
	for d := lo; !hi.Before(d); d = d.AddDays(1) {
		for _, a := range m.heat.days[d] {
			timeW = max(timeW, ansi.StringWidth(m.clock.Time(a.Completed)))
		}
	}
	var lines []string
	for d := lo; !hi.Before(d); d = d.AddDays(1) {
		tasks := m.heat.days[d]
		if len(tasks) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fit(boldStyle.Render(d.In(m.loc).Format("Mon 2 Jan 2006")), width))
		for _, a := range tasks {
			line := "  " + mutedStyle.Render(fit(m.clock.Time(a.Completed), timeW)) + "  " + domain.SafeText(oneLine(a.Text))
			if from := domain.SafeText(oneLine(a.Origin.Title)); from != "" {
				line += mutedStyle.Render("  · from " + from)
			}
			lines = append(lines, fit(line, width))
		}
	}
	if len(lines) == 0 {
		if lo == hi {
			return nil, "No tasks completed on " + lo.In(m.loc).Format("Mon 2 Jan 2006") + "."
		}
		return nil, "No tasks completed on these days."
	}
	return lines, ""
}

// oneLine puts text on one line, however it was written.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// scrollTasks moves the task list by n lines, within what there is.
func (m Model) scrollTasks(n int) Model {
	w, room := m.tasksView()
	lines, _ := m.heatTaskLines(w)
	m.heat.scroll = min(max(m.heat.scroll+n, 0), max(len(lines)-max(room, 1), 0))
	return m
}
