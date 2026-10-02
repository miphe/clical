package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
)

const (
	sidebarInner          = 24 // sidebar width inside its border
	collapsedSidebarInner = 3
	timeColumn            = 11 // display width of "00:00–00:00"
	// itemIndent puts event bullets one column in from day-header text, which
	// sits after a two-column gutter holding the current-day marker.
	itemIndent          = "   "
	minWidth, minHeight = 30, 6
)

// View renders the whole screen.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "clical"
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return "Terminal too small for clical."
	}

	bodyH := m.bodyHeight()
	var body []string
	if m.detail != nil {
		body = m.renderDetail(m.width, bodyH)
	} else {
		side := m.renderSidebar(bodyH)
		main := m.renderMain(m.width-lipgloss.Width(side[0]), bodyH)
		body = make([]string, bodyH)
		for i := range body {
			body[i] = side[i] + main[i]
		}
	}
	switch {
	case m.form != nil:
		body = overlay(body, m.renderForm(), m.width)
	case m.confirmRemove != nil:
		body = overlay(body, m.renderConfirm(), m.width)
	}

	return strings.Join(append(body, m.renderFooter()...), "\n")
}

// fit truncates or pads s to exactly width terminal cells.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

func padLines(lines []string, height, width int) []string {
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lines[:height]
}

// Sidebar.

// renderMain draws the main panel: the current view inside a border whose
// title doubles as the view tabs.
func (m Model) renderMain(width, height int) []string {
	tabs := []tab{
		{label: "[1] Agenda", active: m.view == viewAgenda},
		{label: "[2] Week", active: m.view == viewWeek},
	}
	inner, innerH := width-2, height-2
	var content []string
	var right string
	switch m.view {
	case viewWeek:
		content = m.renderWeek(inner, innerH)
		right = m.weekTitle()
	default:
		// One column of padding between the border and the agenda's gutter.
		content = m.renderAgenda(inner-1, innerH)
		for i := range content {
			content[i] = " " + content[i]
		}
		right = m.agendaSpan()
	}
	return panel(width, height, frame{focused: m.focus == focusMain, tabs: tabs, right: right}, content)
}

// agendaSpan is the loaded agenda window, e.g. "29 Sep – 28 Oct 2026".
func (m Model) agendaSpan() string {
	last := m.from.AddDays(m.days - 1)
	return m.from.In(m.loc).Format("2 Jan") + " – " + last.In(m.loc).Format("2 Jan 2006")
}

// Agenda.

// renderAgenda draws the agenda rows for a width×height area inside the
// main panel.
func (m Model) renderAgenda(width, height int) []string {
	var lines []string
	switch {
	case m.loadErr != nil && len(m.rows) == 0:
		lines = []string{errorStyle.Render("Can't read the local cache: " + m.loadErr.Error())}
	case len(m.rows) == 0 && m.loading:
		lines = []string{dimStyle.Render("Loading…")}
	case len(m.rows) == 0:
		lines = []string{dimStyle.Render(fmt.Sprintf("Nothing scheduled in the %d days from %s.",
			m.days, m.from.In(m.loc).Format("Mon 2 Jan")))}
	default:
		sel := m.selIndex()
		end := min(m.offset+height, len(m.rows))
		for i := m.offset; i < end; i++ {
			lines = append(lines, m.renderRow(m.rows[i], i == sel, width))
		}
	}
	for i := range lines {
		lines[i] = fit(lines[i], width)
	}
	return padLines(lines, height, width)
}

func (m Model) renderRow(r row, selected bool, width int) string {
	switch r.kind {
	case rowDay:
		label := r.date.In(m.loc).Format("Mon 2 Jan")
		style := boldStyle
		if r.date == m.today() {
			label += " · today"
			style = todayStyle
		}
		if r.date == m.currentDay() {
			return style.Render("▸ ") + style.Underline(true).Render(label)
		}
		return "  " + style.Render(label)
	case rowNothing:
		text := itemIndent + "nothing scheduled"
		switch {
		case selected && m.focus == focusMain:
			return itemIndent + selectedStyle.Render("nothing scheduled")
		case selected:
			return itemIndent + inactiveSelStyle.Render("nothing scheduled")
		}
		return dimStyle.Render(text)
	case rowNow:
		label := itemIndent + "── now " + m.now.In(m.loc).Format("15:04") + " "
		return nowStyle.Render(label + strings.Repeat("─", max(0, width-ansi.StringWidth(label))))
	case rowItem:
		return m.renderItem(r, selected, width)
	}
	return ""
}

func (m Model) renderItem(r row, selected bool, width int) string {
	it := r.item
	when := it.TimeLabel(r.date, m.loc)

	label := m.calendarLabel(it.Event.CalendarID)
	labelW := 0
	if width >= 60 {
		labelW = m.labelWidth()
	}
	titleW := width - ansi.StringWidth(itemIndent) - 2 - timeColumn - 2
	if labelW > 0 {
		titleW -= labelW + 2
	}

	plain := func(swatch string) string {
		s := itemIndent + swatch + " " + fit(when, timeColumn) + "  " + fit(it.Event.Title, titleW)
		if labelW > 0 {
			s += "  " + fit(label, labelW)
		}
		return s
	}

	switch {
	case selected && m.focus == focusMain:
		return itemIndent + selectedStyle.Render(fit(strings.TrimPrefix(plain("●"), itemIndent), width-ansi.StringWidth(itemIndent)))
	case selected:
		return itemIndent + inactiveSelStyle.Render(fit(strings.TrimPrefix(plain("●"), itemIndent), width-ansi.StringWidth(itemIndent)))
	}

	ended := !it.Event.AllDay && !it.End.After(m.now)
	swatch := lipgloss.NewStyle().Foreground(m.calendarColor(it.Event.CalendarID)).Render("●")
	s := itemIndent + swatch + " " + fit(when, timeColumn) + "  " + fit(it.Event.Title, titleW)
	if labelW > 0 {
		s += "  " + dimStyle.Render(fit(label, labelW))
	}
	if ended {
		return dimStyle.Render(ansi.Strip(s))
	}
	return s
}

// labelWidth is the width of the calendar-label column: the longest label,
// capped so titles keep most of the room.
func (m Model) labelWidth() int {
	w := 0
	for _, cr := range m.calendars {
		w = max(w, ansi.StringWidth(cr.Label()))
	}
	return min(w, 28)
}

// calendarColor is the color of the calendar an event belongs to.
func (m Model) calendarColor(calendarID string) color.Color {
	if i, ok := m.calIndex[calendarID]; ok && i < len(m.calendars) {
		return calendarColor(m.calendars[i].Calendar.ColorSlot)
	}
	return calendarColor(1)
}

func (m Model) calendarLabel(calendarID string) string {
	i, ok := m.calIndex[calendarID]
	if !ok || i >= len(m.calendars) {
		return ""
	}
	return m.calendars[i].Label()
}

// Event detail.

// renderDetail draws the event detail as a full-width panel.
func (m Model) renderDetail(width, height int) []string {
	e := m.detail.Event
	textW := min(width-4, 76) // inside the border, one column of padding each side

	var parts []string
	parts = append(parts, boldStyle.Render(e.Title))
	parts = append(parts, m.detailWhen())
	if label := m.calendarLabel(e.CalendarID); label != "" {
		sw := lipgloss.NewStyle().Foreground(m.calendarColor(e.CalendarID)).Render("●")
		parts = append(parts, sw+" "+label)
	}
	if e.Location != "" {
		parts = append(parts, "", dimStyle.Render("Location")+"  "+e.Location)
	}
	if notes := strings.TrimSpace(e.Notes); notes != "" {
		parts = append(parts, "", lipgloss.NewStyle().Width(textW).Render(notes))
	}

	var content []string
	for _, line := range strings.Split(strings.Join(parts, "\n"), "\n") {
		content = append(content, " "+line)
	}
	return panel(width, height, frame{focused: true, tabs: []tab{{label: "Event", active: true}}}, content)
}

func (m Model) detailWhen() string {
	e := m.detail.Event
	if ds, de, ok := e.DateRange(); ok {
		last := de.AddDays(-1)
		if last == ds || de == ds {
			return ds.In(m.loc).Format("Mon 2 Jan 2006") + " · all day"
		}
		return ds.In(m.loc).Format("Mon 2 Jan") + " – " + last.In(m.loc).Format("Mon 2 Jan 2006") + " · all day"
	}
	start, end := m.detail.Start, m.detail.End
	dur := formatDuration(end.Sub(start))
	if domain.CivilDateFromTime(start) == domain.CivilDateFromTime(end) || end.Equal(start) {
		s := start.Format("Mon 2 Jan 2006") + " · " + start.Format("15:04")
		if !end.Equal(start) {
			s += "–" + end.Format("15:04") + " (" + dur + ")"
		}
		return s
	}
	return start.Format("Mon 2 Jan 15:04") + " – " + end.Format("Mon 2 Jan 2006 15:04") + " (" + dur + ")"
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	days, hours, mins := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	return strings.Join(parts, " ")
}

// Footer.

func (m Model) renderFooter() []string {
	hint := func(k, desc string) key.Binding { return key.NewBinding(key.WithHelp(k, desc)) }
	var bindings []key.Binding
	switch {
	case m.form != nil:
		bindings = []key.Binding{hint("enter", "next/add"), hint("tab", "move"), hint("esc", "cancel")}
	case m.confirmRemove != nil:
		bindings = []key.Binding{hint("y", "remove"), hint("n", "cancel")}
	case m.detail != nil:
		bindings = []key.Binding{m.keys.Close}
	case m.focus == focusSidebar:
		bindings = []key.Binding{m.keys.Toggle, hint("enter", "fold"), m.keys.Add, m.keys.Remove,
			m.keys.Collapse, m.keys.FocusMain, m.keys.Quit}
	case m.view == viewWeek:
		bindings = []key.Binding{hint("h/l", "day"), hint("j/k", "move"), m.keys.Open, hint("[/]", "week"),
			m.keys.Today, hint("tab", "agenda"), m.keys.Sync, m.keys.Quit}
	default:
		bindings = []key.Binding{hint("j/k", "move"), m.keys.Open, hint("[/]", "day"), m.keys.Today,
			hint("tab", "week"), m.keys.Sync, m.keys.FocusSidebar, m.keys.Quit}
	}
	var hints []string
	for _, b := range bindings {
		h := b.Help()
		hints = append(hints, boldStyle.Render(h.Key)+" "+dimStyle.Render(h.Desc))
	}
	hintLine := fit(" "+strings.Join(hints, dimStyle.Render(" · ")), m.width)

	left := " " + m.activity() + m.now.In(m.loc).Format("Mon 2 Jan 15:04")
	right := m.status()
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	var statusLine string
	if gap >= 2 {
		statusLine = left + strings.Repeat(" ", gap) + right + " "
	} else {
		statusLine = fit(left+"  "+right, m.width)
	}
	return []string{hintLine, fit(statusLine, m.width)}
}

func (m Model) status() string {
	switch {
	case m.loadErr != nil:
		return errorStyle.Render("Can't read cache: " + m.loadErr.Error())
	case m.actionErr != nil:
		return errorStyle.Render(m.actionErr.Error())
	case m.syncErr != nil:
		return errorStyle.Render("Sync failed: " + m.syncErr.Error())
	case len(m.syncFailures) > 0:
		msg := m.syncFailures[0]
		if n := len(m.syncFailures) - 1; n > 0 {
			msg += fmt.Sprintf(" (+%d more)", n)
		}
		return errorStyle.Render(msg)
	case !m.lastSync.IsZero():
		return dimStyle.Render("Synced " + m.lastSync.In(m.loc).Format("15:04"))
	}
	return ""
}
