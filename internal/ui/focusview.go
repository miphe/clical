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
	heatPanelH    = heatRows + 2     // the heatmap and its border
	minTasksPanel = 5                // below this a panel above the heatmap is dropped
	agendaPanelW  = sidebarInner + 2 // the agenda is as wide as the calendar list
	minTasksW     = 52               // the view tabs need this much to show in full
	minAgendaH    = 5
	maxAgendaH    = 8
)

// focusLayout is where the focus view's panels go. Wide: the agenda and the
// tasks side by side above the heatmap. Narrower: the agenda on top, then the
// tasks, then the heatmap. Short: panels above the heatmap are dropped, the
// tasks first, and the heatmap is always kept.
type focusLayout struct {
	agendaW, agendaH int // 0 when absent
	tasksW, tasksH   int
	heatH            int
	stacked          bool
}

func (m Model) focusLayout() focusLayout {
	w, h := m.mainWidth(), m.bodyHeight()
	rest := h - heatPanelH
	if rest < minTasksPanel {
		return focusLayout{heatH: h}
	}
	if w >= agendaPanelW+minTasksW {
		return focusLayout{agendaW: agendaPanelW, agendaH: rest, tasksW: w - agendaPanelW, tasksH: rest, heatH: heatPanelH}
	}
	agendaH := min(max(rest/3, minAgendaH), maxAgendaH)
	if rest-agendaH < minTasksPanel {
		return focusLayout{agendaW: w, agendaH: rest, heatH: heatPanelH, stacked: true}
	}
	return focusLayout{agendaW: w, agendaH: agendaH, tasksW: w, tasksH: rest - agendaH, heatH: heatPanelH, stacked: true}
}

// renderFocus draws the focus view's panels in a width×height area. The view
// tabs go in the top edge of the panel that leads: the tasks beside the
// agenda, the agenda when stacked, the heatmap when it is alone.
func (m Model) renderFocus(width, height int, tabs []tab) []string {
	lay := m.focusLayout()
	agendaTabs := []tab{{label: "Next", active: true}}
	tasksTabs := []tab{{label: "Completed", active: true}}
	heatTabs := []tab{{label: "Heatmap", active: true}}
	switch {
	case lay.agendaH == 0:
		heatTabs = tabs
	case lay.stacked:
		agendaTabs = tabs
	default:
		tasksTabs = tabs
	}

	var out []string
	if lay.agendaH > 0 {
		agenda := m.renderAgendaPanel(lay.agendaW, lay.agendaH, agendaTabs)
		switch {
		case lay.stacked:
			out = append(out, agenda...)
			if lay.tasksH > 0 {
				out = append(out, m.renderTasks(lay.tasksW, lay.tasksH, tasksTabs)...)
			}
		default:
			tasks := m.renderTasks(lay.tasksW, lay.tasksH, tasksTabs)
			for i := range agenda {
				out = append(out, agenda[i]+tasks[i])
			}
		}
	}
	heat := m.renderHeat(width-2, lay.heatH-2)
	out = append(out, panel(width, lay.heatH, frame{focused: m.focus == focusHeat, tabs: heatTabs, legend: heatLegend()}, heat)...)
	return padLines(out, height, width)
}

// renderAgendaPanel is the agenda, the same rows as view 1, in a panel.
func (m Model) renderAgendaPanel(width, height int, tabs []tab) []string {
	inner, innerH := width-2, height-2
	content := m.renderAgenda(inner-1, innerH) // a column of padding, as in view 1
	for i := range content {
		content[i] = " " + content[i]
	}
	return panel(width, height, frame{focused: m.focus == focusMain, tabs: tabs}, content)
}

// tasksView is the room for the task lines: inside the border, and under the
// header and its blank line when the selection has one.
func (m Model) tasksView() (width, height int) {
	lay := m.focusLayout()
	height = lay.tasksH - 2
	if m.heatHasHeader() {
		height -= 2
	}
	return lay.tasksW - 2, height
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
		// The day is named in bold, in the color of its square in the heatmap
		// (not faint: a faint name would be hard to read).
		label := heatStyles[heatLevel(len(tasks))].Faint(false).Bold(true)
		lines = append(lines, fit(label.Render(d.In(m.loc).Format("Mon 2 Jan 2006")), width))
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
