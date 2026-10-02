package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	case m.gflow != nil:
		body = overlay(body, m.renderGoogleFlow(), m.width)
	case m.chooser != nil:
		body = overlay(body, m.renderChooser(), m.width)
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
	monthLabel := "[3] Month"
	if m.view == viewMonth {
		monthLabel = "[3] " + m.month.month.In(m.loc).Format("January 2006")
	}
	weekLabel := "[2] Week"
	if m.view == viewWeek && m.weekNumber() > 0 {
		weekLabel = fmt.Sprintf("[2] Week %d", m.weekNumber())
	}
	tabs := []tab{
		{label: "[1] Agenda", active: m.view == viewAgenda},
		{label: weekLabel, active: m.view == viewWeek},
		{label: monthLabel, active: m.view == viewMonth},
	}
	inner, innerH := width-2, height-2
	var content []string
	var right string
	switch m.view {
	case viewWeek:
		content = m.renderWeek(inner, innerH)
		right = m.weekSpan()
	case viewMonth:
		content = m.renderMonth(inner, innerH)
		right = m.monthWeekSpan()
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
		lines = []string{mutedStyle.Render("Loading…")}
	case len(m.rows) == 0:
		lines = []string{mutedStyle.Render(fmt.Sprintf("Nothing scheduled in the %d days from %s.",
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
		return mutedStyle.Render(text)
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

	slot := m.calendarSlot(it.Event.CalendarID)
	glyph := calendarGlyph(slot, false)
	plain := func(swatch string) string {
		s := itemIndent + swatch + " " + fit(when, timeColumn) + "  " + fit(it.Event.Title, titleW)
		if labelW > 0 {
			s += "  " + fit(label, labelW)
		}
		return s
	}

	switch {
	case selected && m.focus == focusMain:
		return itemIndent + selectedStyle.Render(fit(strings.TrimPrefix(plain(glyph), itemIndent), width-ansi.StringWidth(itemIndent)))
	case selected:
		return itemIndent + inactiveSelStyle.Render(fit(strings.TrimPrefix(plain(glyph), itemIndent), width-ansi.StringWidth(itemIndent)))
	}

	ended := !it.Event.AllDay && !it.End.After(m.now)
	swatch := calendarBullet(slot)
	s := itemIndent + swatch + " " + fit(when, timeColumn) + "  " + fit(it.Event.Title, titleW)
	if labelW > 0 {
		s += "  " + mutedStyle.Render(fit(label, labelW))
	}
	if ended {
		return mutedStyle.Render(ansi.Strip(s))
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

// calendarSlot is the color slot of the calendar an event belongs to.
func (m Model) calendarSlot(calendarID string) int {
	if i, ok := m.calIndex[calendarID]; ok && i < len(m.calendars) {
		return m.calendars[i].Calendar.ColorSlot
	}
	return 1
}

func (m Model) calendarLabel(calendarID string) string {
	i, ok := m.calIndex[calendarID]
	if !ok || i >= len(m.calendars) {
		return ""
	}
	return m.calendars[i].Label()
}

// Footer.

func (m Model) renderFooter() []string {
	hint := func(k, desc string) key.Binding { return key.NewBinding(key.WithHelp(k, desc)) }
	var bindings []key.Binding
	switch {
	case m.form != nil:
		bindings = []key.Binding{hint("enter", "next/add"), hint("tab", "move"), hint("esc", "cancel")}
	case m.gflow != nil || m.chooser != nil:
		bindings = []key.Binding{hint("esc", "cancel")}
	case m.confirmRemove != nil:
		bindings = []key.Binding{hint("y", "remove"), hint("n", "cancel")}
	case m.detail != nil && len(m.detail.Event.Links) > 0:
		bindings = []key.Binding{hint("j/k", "select link"), hint("enter", "open"), hint("pgup/pgdn", "scroll"), m.keys.Close}
	case m.detail != nil:
		bindings = []key.Binding{hint("j/k", "scroll"), m.keys.Close}
	case m.focus == focusSidebar:
		bindings = []key.Binding{hint("j/k", "move"), m.keys.Toggle, hint("enter", "fold"), m.keys.Add, m.keys.Remove}
		if acct, ok := m.selectedGoogleAccount(); ok && m.needsSignIn[acct.ID] {
			bindings = append([]key.Binding{hint("s", "sign in again")}, bindings...)
		}
		bindings = append(bindings, m.keys.Collapse, m.keys.Sync, m.keys.Quit)
	case m.view == viewWeek:
		bindings = []key.Binding{hint("h/l", "day"), hint("j/k", "move"), m.keys.Open, hint("[/]", "week"),
			m.keys.Today, m.keys.Sync, m.keys.Quit}
	case m.view == viewMonth:
		bindings = []key.Binding{hint("h/l", "day"), hint("j/k", "week"), hint("enter", "open day"), hint("[/]", "month"),
			m.keys.Today, m.keys.Sync, m.keys.Quit}
	default:
		bindings = []key.Binding{hint("j/k", "move"), m.keys.Open, hint("[/]", "day"), m.keys.Today, m.keys.Sync, m.keys.Quit}
	}
	render := func(bs []key.Binding) string {
		var hints []string
		for _, b := range bs {
			h := b.Help()
			hints = append(hints, boldStyle.Render(h.Key)+" "+mutedStyle.Render(h.Desc))
		}
		return strings.Join(hints, mutedStyle.Render(" · "))
	}
	line := render(bindings)
	if m.form == nil && m.gflow == nil && m.chooser == nil && m.confirmRemove == nil && m.detail == nil {
		// Pane keys first, the same everywhere, so they're never cut off.
		panes := render([]key.Binding{hint("0", "calendars"), hint("1-3", "views"), hint("tab", "switch pane")})
		line = panes + mutedStyle.Render(" │ ") + line
	}
	hintLine := fit(" "+line, m.width)

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

// retryNote says when a failed sync will be tried again.
func (m Model) retryNote() string {
	if m.nextPoll.IsZero() || m.syncing {
		return ""
	}
	return mutedStyle.Render(" · retry " + m.nextPoll.In(m.loc).Format("15:04"))
}

func (m Model) status() string {
	switch {
	case m.loadErr != nil:
		return errorStyle.Render("Can't read cache: " + m.loadErr.Error())
	case m.actionErr != nil:
		return errorStyle.Render(m.actionErr.Error())
	case m.syncErr != nil:
		return errorStyle.Render("Sync failed: "+m.syncErr.Error()) + m.retryNote()
	case len(m.syncFailures) > 0:
		failures := m.failureList()
		msg := failures[0]
		if n := len(failures) - 1; n > 0 {
			msg += fmt.Sprintf(" (+%d more)", n)
		}
		return errorStyle.Render(msg) + m.retryNote()
	case !m.lastSync.IsZero():
		return mutedStyle.Render("Synced " + m.lastSync.In(m.loc).Format("15:04"))
	}
	return ""
}
