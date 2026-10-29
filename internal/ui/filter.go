package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/domain"
)

// The filter (/) narrows every view to events whose title or location
// contains each typed word, case-insensitively, as you type. enter keeps the
// filter and returns to moving around; esc clears it. While it's on, the
// agenda searches a year ahead and leaves out days without matches.

const filterDays = 365

func newFilterInput(value string) textinput.Model {
	in := textinput.New()
	in.SetStyles(inputStyles())
	in.Prompt = ""
	in.CharLimit = 200
	in.Placeholder = "title or location"
	in.SetValue(value)
	in.CursorEnd()
	return in
}

// filterTerms is the filter split into lower-case words; nil when off.
func (m Model) filterTerms() []string {
	return strings.Fields(strings.ToLower(m.filter))
}

// shows reports whether e is shown: its calendar isn't hidden and it
// matches the filter.
func (m Model) shows(e domain.Event) bool {
	if m.hidden[e.CalendarID] {
		return false
	}
	terms := m.filterTerms()
	if len(terms) == 0 {
		return true
	}
	text := strings.ToLower(e.Title + "\n" + e.Location)
	for _, t := range terms {
		if !strings.Contains(text, t) {
			return false
		}
	}
	return true
}

func (m Model) startFilter() (tea.Model, tea.Cmd) {
	m.focus = focusMain
	in := newFilterInput(m.filter)
	m.filterInput = &in
	return m, in.Focus()
}

func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filterInput = nil
		return m.setFilter("")
	case "enter":
		m.filterInput = nil
		return m, nil
	}
	var cmd tea.Cmd
	*m.filterInput, cmd = m.filterInput.Update(msg)
	next, load := m.setFilter(m.filterInput.Value())
	return next, tea.Batch(cmd, load)
}

func (m Model) filterPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	*m.filterInput, cmd = m.filterInput.Update(msg)
	next, load := m.setFilter(m.filterInput.Value())
	return next, tea.Batch(cmd, load)
}

// setFilter applies a new filter to every view, loading a year of agenda
// the first time one is set.
func (m Model) setFilter(value string) (tea.Model, tea.Cmd) {
	m.filter = strings.TrimSpace(value)
	m.rebuild()
	m.rebuildWeek()
	m.rebuildWeek()
	m.rebuildMonth()
	if m.filter == "" || m.days >= filterDays {
		return m, nil
	}
	m.days = filterDays
	m.loading = true
	return m, m.loadEvents()
}

// filterLine replaces the footer's hint line while typing a filter.
func (m Model) filterLine() string {
	in := *m.filterInput
	in.SetWidth(max(min(40, m.width-34), 10))
	return accentStyle.Render("/") + in.View() + "  " + renderHints([]footHint{
		{key: "enter", desc: "keep"}, {key: "esc", desc: "clear"},
	})
}

// noMatches is the agenda's text when the filter matches nothing loaded.
func (m Model) noMatches() string {
	return fmt.Sprintf("No events match “%s” in the %d days from %s.", m.filter, m.days, m.from.In(m.loc).Format("Mon 2 Jan"))
}
