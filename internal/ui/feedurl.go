package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/miphe/clical/internal/app"
)

// The new-URL popup (u on an iCal calendar in the list) replaces a
// calendar's secret address, typically after it was reset and the old one
// stopped working (the calendar is marked ⚠). The calendar keeps its name,
// color and place; the new address is checked, saved and synced.

type urlForm struct {
	cal      app.CalendarInfo
	input    textinput.Model
	checking bool
	err      string
}

type urlDoneMsg struct {
	cal app.CalendarInfo
	err error
}

func (m Model) startURLForm() (tea.Model, tea.Cmd) {
	r, ok := m.selectedSidebarRow()
	if !ok || r.kind != sideCalendar || r.cal.Account.Provider != "ical" {
		return m, nil
	}
	in := textinput.New()
	in.SetStyles(inputStyles())
	in.Prompt = ""
	in.CharLimit = 4096
	in.Placeholder = "https://calendar.google.com/…/basic.ics"
	// The URL is a credential: never shown on screen.
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	m.urlForm = &urlForm{cal: r.cal, input: in}
	return m, m.urlForm.input.Focus()
}

func (m Model) handleURLKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.urlForm
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if !f.checking {
			m.urlForm = nil
		}
		return m, nil
	}
	if f.checking {
		return m, nil
	}
	if msg.String() == "enter" {
		raw := strings.TrimSpace(f.input.Value())
		if raw == "" {
			f.err = "Paste the calendar's new secret address."
			return m, nil
		}
		f.checking, f.err = true, ""
		src, cal := m.src, f.cal
		return m, func() tea.Msg {
			ctx := context.Background()
			feed, err := src.CheckFeed(ctx, raw)
			if err == nil {
				err = src.SetCalendarURL(ctx, cal, feed)
			}
			return urlDoneMsg{cal: cal, err: err}
		}
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	f.err = ""
	return m, cmd
}

func (m Model) urlPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.urlForm.checking {
		return m, nil
	}
	var cmd tea.Cmd
	m.urlForm.input, cmd = m.urlForm.input.Update(msg)
	m.urlForm.err = ""
	return m, cmd
}

func (m Model) urlDone(msg urlDoneMsg) (tea.Model, tea.Cmd) {
	if m.urlForm == nil {
		return m, nil
	}
	if msg.err != nil {
		m.urlForm.checking, m.urlForm.err = false, describeAddError(msg.err)
		return m, nil
	}
	m.urlForm = nil
	m.syncing = true
	return m, m.syncCalendarCmd(msg.cal)
}

func (m Model) renderURLForm() []string {
	f := m.urlForm
	w := min(m.width-4, 64)
	inner := w - 4
	f.input.SetWidth(inner - 1)
	lines := []string{
		" " + boldStyle.Render("New secret iCal URL for ") + f.cal.Label(),
		" " + f.input.View(),
		"",
	}
	note := "Replaces the address after it was reset. The calendar keeps its name and color."
	style := mutedStyle
	switch {
	case f.checking:
		note, style = "Checking the feed…", busyStyle
	case f.err != "":
		note, style = f.err, errorStyle
	}
	for _, l := range strings.Split(lipgloss.NewStyle().Width(inner).Render(note), "\n") {
		lines = append(lines, " "+style.Render(l))
	}
	return panel(w, len(lines)+2, frame{focused: true, tabs: []tab{{label: "Calendar address", active: true}}}, lines)
}
