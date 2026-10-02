package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

// The rename popup gives a calendar or an account the user's own name
// (n on a row in the calendar list). The name only changes what clical
// shows; see app.RenameCalendar.

const maxNameLength = 100

type renameForm struct {
	input   textinput.Model
	account bool // renaming the account, not the calendar
	cal     app.CalendarInfo
	acct    domain.Account
	saving  bool
	err     string
}

type renameDoneMsg struct{ err error }

func (r *renameForm) own() string {
	if r.account {
		return r.acct.Label
	}
	return r.cal.Calendar.Name
}

// startRename opens the popup for the selected sidebar row.
func (m Model) startRename() (tea.Model, tea.Cmd) {
	row, ok := m.selectedSidebarRow()
	if !ok {
		return m, nil
	}
	f := &renameForm{}
	current := ""
	if row.kind == sideAccount {
		for _, c := range m.calendars {
			if c.Account.ID == row.account {
				f.account, f.acct = true, c.Account
				break
			}
		}
		if !f.account {
			return m, nil
		}
		current = f.acct.DisplayName()
	} else {
		f.cal = row.cal
		current = f.cal.Calendar.DisplayName()
	}
	f.input = textinput.New()
	f.input.SetStyles(inputStyles())
	f.input.Prompt = ""
	f.input.CharLimit = maxNameLength
	f.input.Placeholder = f.own()
	f.input.SetValue(current)
	f.input.CursorEnd()
	m.rename = f
	return m, f.input.Focus()
}

func (m Model) handleRenameKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.rename
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if !f.saving {
			m.rename = nil
		}
		return m, nil
	}
	if f.saving {
		return m, nil
	}
	if msg.String() == "enter" {
		f.saving, f.err = true, ""
		src, name, form := m.src, f.input.Value(), *f
		return m, func() tea.Msg {
			var err error
			if form.account {
				_, err = src.RenameAccount(context.Background(), form.acct, name)
			} else {
				_, err = src.RenameCalendar(context.Background(), form.cal, name)
			}
			return renameDoneMsg{err: err}
		}
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	f.err = ""
	return m, cmd
}

func (m Model) renamePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.rename.saving {
		return m, nil
	}
	var cmd tea.Cmd
	m.rename.input, cmd = m.rename.input.Update(msg)
	m.rename.err = ""
	return m, cmd
}

func (m Model) renameDone(msg renameDoneMsg) (tea.Model, tea.Cmd) {
	if m.rename == nil {
		return m, nil
	}
	if msg.err != nil {
		m.rename.saving, m.rename.err = false, msg.err.Error()
		return m, nil
	}
	m.rename = nil
	// Event rows, the week and the month take calendar names from the
	// list, so reloading it renames them everywhere.
	return m, m.loadCalendars()
}

func (m Model) renderRename() []string {
	f := m.rename
	w := min(m.width-4, 64)
	inner := w - 4
	title, label := "Rename calendar", "Name"
	if f.account {
		title = "Rename account"
	}
	f.input.SetWidth(inner - 1)
	lines := []string{" " + boldStyle.Render(label), " " + f.input.View(), ""}
	note := "Its own name is " + f.own() + "; leave empty to use it."
	if f.account && strings.Contains(f.own(), "@") {
		note += " Signing in again still uses this address."
	}
	switch {
	case f.saving:
		lines = append(lines, " "+busyStyle.Render("Saving…"))
	case f.err != "":
		for _, l := range strings.Split(lipgloss.NewStyle().Width(inner).Render(f.err), "\n") {
			lines = append(lines, " "+errorStyle.Render(l))
		}
	default:
		for _, l := range strings.Split(lipgloss.NewStyle().Width(inner).Render(note), "\n") {
			lines = append(lines, " "+mutedStyle.Render(l))
		}
	}
	return panel(w, len(lines)+2, frame{focused: true, tabs: []tab{{label: title, active: true}}}, lines)
}
