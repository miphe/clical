package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	sidebarInner          = 24 // sidebar width inside its border
	collapsedSidebarInner = 3
	// itemIndent puts event bullets one column in from day-header text, which
	// sits after a two-column gutter holding the current-day marker.
	itemIndent          = "   "
	minWidth, minHeight = 30, 6
	// An agenda narrower than this lays its rows out compactly.
	compactAgendaWidth = 40
	compactWhenW       = 7 // "all day", "12:00pm"
)

// View renders the whole screen.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "clical"
	if m.notice != "" {
		v.WindowTitle += " (" + strings.ToLower(m.notice) + ")"
	}
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
	if m.settings != nil {
		body = m.renderSettings(m.width, bodyH)
	} else if m.help != nil {
		body = m.renderHelp(m.width, bodyH)
	} else if m.detail != nil {
		body = m.renderEventView(m.width, bodyH)
	} else {
		side := m.renderSidebar(bodyH)
		gw, _ := m.globalRect()
		main := m.renderMain(m.width-lipgloss.Width(side[0])-gw, bodyH)
		glob := m.renderGlobalPanel(gw, bodyH)
		body = make([]string, bodyH)
		for i := range body {
			body[i] = side[i] + main[i] + glob[i]
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
	case m.rename != nil:
		body = overlay(body, m.renderRename(), m.width)
	case m.urlForm != nil:
		body = overlay(body, m.renderURLForm(), m.width)
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
	if m.heatEnabled() {
		tabs = append(tabs, tab{label: "[4] Focus", active: m.view == viewHeat})
	}
	inner, innerH := width-2, height-2
	var content []string
	var right, bottom string
	switch m.view {
	case viewHeat:
		return m.renderFocus(width, height, tabs)
	case viewWeek:
		content = m.renderWeek(inner, innerH)
		right = m.weekSpan()
		bottom = fmt.Sprintf("%d min/row", m.weekSlot())
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
	return panel(width, height, frame{focused: m.focus == focusMain, tabs: tabs, right: right, bottom: bottom}, content)
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
	case len(m.rows) == 0 && m.filter != "":
		lines = []string{mutedStyle.Render(m.noMatches())}
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
		label := itemIndent + "── now " + m.clock.Time(m.now.In(m.loc)) + " "
		return nowStyle.Render(label + strings.Repeat("─", max(0, width-ansi.StringWidth(label))))
	case rowItem:
		return m.renderItem(r, selected, width)
	}
	return ""
}

func (m Model) renderItem(r row, selected bool, width int) string {
	it := r.item
	when := it.TimeLabel(r.date, m.loc, m.clock)

	label := m.calendarLabel(it.Event.CalendarID)
	labelW := 0
	if width >= 60 {
		labelW = m.labelWidth()
	}
	// In a narrow panel (the focus view's agenda) a row is tighter: a smaller
	// indent, only the start time, and one space before the title.
	indent, whenW, gap := itemIndent, m.clock.Width(), "  "
	if width < compactAgendaWidth {
		indent, whenW, gap = " ", compactWhenW, " "
		when = strings.SplitN(when, "–", 2)[0]
	}
	titleW := width - ansi.StringWidth(indent) - 2 - whenW - len(gap)
	if labelW > 0 {
		titleW -= labelW + 2
	}

	slot := m.calendarSlot(it.Event.CalendarID)
	glyph := calendarGlyph(slot, false)
	plain := func(swatch string) string {
		s := indent + swatch + " " + fit(when, whenW) + gap + fit(it.Event.Title, titleW)
		if labelW > 0 {
			s += "  " + fit(label, labelW)
		}
		return s
	}

	switch {
	case m.isPulsing(it.Event):
		return indent + m.pulseLine(it.Event, strings.TrimPrefix(plain(glyph), indent))
	case selected && m.focus == focusMain:
		return indent + selectedStyle.Render(fit(strings.TrimPrefix(plain(glyph), indent), width-ansi.StringWidth(indent)))
	case selected:
		return indent + inactiveSelStyle.Render(fit(strings.TrimPrefix(plain(glyph), indent), width-ansi.StringWidth(indent)))
	}

	ended := !it.Event.AllDay && !it.End.After(m.now)
	swatch := calendarBullet(slot)
	s := indent + swatch + " " + fit(when, whenW) + gap + fit(it.Event.Title, titleW)
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
	line := fitHints(m.shownHints(), m.width-1)
	if m.filterInput != nil {
		line = m.filterLine()
	}
	if m.findInput != nil {
		line = m.findLine()
	}
	hintLine := fit(" "+line, m.width)

	left := " " + m.activity() + m.now.In(m.loc).Format("Mon 2 Jan ") + m.clock.Time(m.now.In(m.loc))
	if m.notice != "" {
		left += "  " + accentStyle.Render(m.notice)
	}
	right := m.status()
	if alert := m.pulseAlert(); alert != "" {
		right = alert
	}
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
	return mutedStyle.Render(" · retry " + m.clock.Time(m.nextPoll.In(m.loc)))
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
		return mutedStyle.Render("Synced " + m.clock.Time(m.lastSync.In(m.loc)))
	}
	return ""
}

// A footer hint, and how much it matters when the line is too narrow for
// all of them: the least important go first, the rest keep their order.
type footHint struct {
	key, desc string
	prio      int
	newGroup  bool // starts a group, set apart by │
}

const (
	prioLow  = iota + 1 // nice to know; also in ? help
	prioMid             // frequent
	prioHigh            // what this screen is for
	prioMust            // never dropped
)

// shownHints is footerHints without the Write ones when the feature is off.
func (m Model) shownHints() []footHint {
	hints := m.footerHints()
	if m.writeOn {
		return hints
	}
	var out []footHint
	for _, h := range hints {
		if h.desc != "write" {
			out = append(out, h)
		}
	}
	return out
}

func (m Model) footerHints() []footHint {
	h := func(k, desc string, prio int) footHint { return footHint{key: k, desc: desc, prio: prio} }
	help := h("?", "help", prioMust)
	switch {
	case m.form != nil:
		return []footHint{h("enter", "next/add", prioMust), h("tab", "move", prioMid), h("esc", "cancel", prioMust)}
	case m.gflow != nil || m.chooser != nil:
		return []footHint{h("esc", "cancel", prioMust)}
	case m.confirmRemove != nil:
		return []footHint{h("y", "remove", prioMust), h("n", "cancel", prioMust)}
	case m.rename != nil:
		return []footHint{h("enter", "save", prioMust), h("esc", "cancel", prioMust)}
	case m.urlForm != nil:
		return []footHint{h("enter", "check and save", prioMust), h("esc", "cancel", prioMust)}
	case m.writeTyping():
		if m.focusedPanel().mode == wConfirm {
			return []footHint{h("y", "delete", prioMust), h("any key", "keep", prioMust)}
		}
		if m.focusedPanel().editing() {
			return []footHint{h("enter", "save", prioMust), h("esc", "cancel", prioMust)}
		}
		return []footHint{h("enter", "add", prioMust), h("esc", "done", prioMust)}
	case m.focusedPanel() != nil:
		return []footHint{h("j/k", "move", prioMid), h("a", "task", prioHigh), h("A", "note", prioHigh), h("space", "check", prioHigh), h("!", "priority", prioMid),
			h("e", "edit", prioMid), h("d", "delete", prioMid), h("m", "move", prioMid), h("alt+j/k", "reorder", prioLow), h("z", "archive", prioMid), h("Z", "archive all", prioLow),
			h("esc", "back", prioMust), help}
	case m.settings != nil && m.settings.input != nil:
		return []footHint{h("enter", "save", prioMust), h("esc", "cancel", prioMust)}
	case m.settings != nil:
		return []footHint{h("j/k", "move", prioMid), h("space", "change", prioMust), h("esc", "close", prioMust)}
	case m.help != nil:
		return []footHint{h("j/k", "scroll", prioMid), h("pgup/pgdn", "page", prioLow), h("esc", "close", prioMust)}
	case m.detail != nil:
		var hints []footHint
		if len(m.detail.Event.Links) > 0 {
			hints = append(hints, h("j/k", "select link", prioMid), h("enter", "open", prioHigh))
		} else {
			hints = append(hints, h("j/k", "scroll", prioMid))
		}
		if len(m.detail.Event.Guests) > 0 {
			hints = append(hints, h("/", "find guest", prioHigh))
		}
		hints = append(hints, h("w", "write", prioHigh))
		if len(m.detail.Event.Links) > 0 {
			hints = append(hints, h("pgup/pgdn", "scroll", prioLow))
		}
		if m.detail.find != "" {
			return append(hints, h("esc", "clear find", prioMust), help)
		}
		return append(hints, h("esc", "close", prioMust), help)
	}

	// Pane keys first, the same everywhere.
	views := "1-3"
	if m.heatEnabled() {
		views = "1-4"
	}
	hints := []footHint{h("0", "calendars", prioHigh), h("1-3", "views", prioHigh), h("tab", "switch pane", prioMid)}
	hints[1].key = views
	if m.filter != "" {
		// The active filter leads, so it's clear why events are missing.
		hints = append([]footHint{h("/"+m.filter, "", prioMust), h("esc", "clear", prioMust)}, hints...)
		hints[2].newGroup = true
	}
	var view []footHint
	switch {
	case m.focus == focusSidebar:
		if acct, ok := m.selectedGoogleAccount(); ok && m.needsSignIn[acct.ID] {
			view = append(view, h("s", "sign in again", prioHigh))
		}
		if r, ok := m.selectedSidebarRow(); ok && r.kind == sideCalendar && m.brokenFeed[r.cal.Calendar.ID] {
			view = append(view, h("u", "new URL", prioHigh))
		}
		view = append(view, h("j/k", "move", prioLow), h("space", "show/hide", prioHigh), h("enter", "fold", prioLow),
			h("a", "add", prioHigh), h("n", "rename", prioMid), h("d", "remove", prioMid), h("c", "collapse", prioLow))
	case m.focus == focusTasks:
		view = []footHint{h("j/k", "scroll", prioHigh), h("pgup/pgdn", "page", prioMid), h("esc", "back", prioMid)}
	case m.view == viewHeat && m.focus != focusMain:
		view = []footHint{h("h/l", "week", prioMid), h("j/k", "day", prioMid), h("shift", "extend", prioHigh),
			h("space", "day/week/month", prioHigh), h("pgup/pgdn", "scroll tasks", prioLow), h("t", "today", prioMid)}
	case m.view == viewWeek:
		view = []footHint{h("h/l", "day", prioMid), h("j/k", "move", prioLow), h("enter", "details", prioHigh),
			h("+/-", "zoom", prioMid), h("D", "5/7 days", prioLow), h("[/]", "week", prioMid), h("t", "today", prioMid), h("/", "filter", prioMid)}
	case m.view == viewMonth:
		view = []footHint{h("h/l", "day", prioLow), h("j/k", "week", prioLow), h("enter", "open day", prioHigh),
			h("[/]", "month", prioMid), h("t", "today", prioMid), h("/", "filter", prioMid)}
	default:
		view = []footHint{h("j/k", "move", prioLow), h("enter", "details", prioHigh), h("[/]", "day", prioMid), h("t", "today", prioMid),
			h("/", "filter", prioMid)}
	}
	view = append(view, h("W", "write", prioMid), h("r", "sync", prioLow), h("S", "settings", prioLow), h("q", "quit", prioMid), help)
	view[0].newGroup = true
	return append(hints, view...)
}

// fitHints renders hints in width, leaving out the least important (the
// rightmost first among equals) until they fit. If even the essential ones
// don't, the line is cut.
func fitHints(hints []footHint, width int) string {
	hints = append([]footHint(nil), hints...)
	for {
		line := renderHints(hints)
		if ansi.StringWidth(line) <= width {
			return line
		}
		drop := -1
		for i, hn := range hints {
			if hn.prio < prioMust && (drop < 0 || hn.prio <= hints[drop].prio) {
				drop = i
			}
		}
		if drop < 0 {
			return line
		}
		if hints[drop].newGroup && drop+1 < len(hints) {
			hints[drop+1].newGroup = true
		}
		hints = append(hints[:drop], hints[drop+1:]...)
	}
}

func renderHints(hints []footHint) string {
	var b strings.Builder
	for i, hn := range hints {
		if i > 0 {
			if hn.newGroup {
				b.WriteString(mutedStyle.Render(" │ "))
			} else {
				b.WriteString(mutedStyle.Render(" · "))
			}
		}
		b.WriteString(boldStyle.Render(hn.key))
		if hn.desc != "" {
			b.WriteString(" " + mutedStyle.Render(hn.desc))
		}
	}
	return b.String()
}
