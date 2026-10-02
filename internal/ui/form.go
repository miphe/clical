package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

// The add-calendar form and the remove confirmation are popups drawn over
// the screen. While one is open it gets every key.

const (
	fieldAccount = iota
	fieldName
	fieldURL
	fieldCount
)

var fieldLabels = [fieldCount]string{"Account", "Calendar name", "Secret iCal URL"}

type addForm struct {
	inputs   [fieldCount]textinput.Model
	focus    int
	checking bool   // feed check and save in flight
	err      string // shown under the fields
}

type (
	addDoneMsg struct {
		cal    app.CalendarInfo
		events int
		err    error
	}
	removeDoneMsg struct {
		cal app.CalendarInfo
		err error
	}
)

func newAddForm(account string) *addForm {
	f := &addForm{}
	for i := range f.inputs {
		in := textinput.New()
		in.Prompt = ""
		in.CharLimit = 4096
		f.inputs[i] = in
	}
	f.inputs[fieldAccount].SetValue(account)
	f.inputs[fieldName].Placeholder = "e.g. Main"
	f.inputs[fieldURL].Placeholder = "https://calendar.google.com/…/basic.ics"
	// The URL is a credential: never shown on screen.
	f.inputs[fieldURL].EchoMode = textinput.EchoPassword
	f.inputs[fieldURL].EchoCharacter = '•'

	f.focus = fieldName
	if account == "" {
		f.focus = fieldAccount
	}
	return f
}

func (f *addForm) focusField(i int) tea.Cmd {
	f.focus = (i + fieldCount) % fieldCount
	var cmd tea.Cmd
	for j := range f.inputs {
		if j == f.focus {
			cmd = f.inputs[j].Focus()
		} else {
			f.inputs[j].Blur()
		}
	}
	return cmd
}

// openAddForm opens the form, defaulting the account to the one under the
// sidebar cursor (or the first account).
func (m Model) openAddForm() (tea.Model, tea.Cmd) {
	account := ""
	if r, ok := m.selectedSidebarRow(); ok {
		if r.kind == sideAccount {
			account = r.label
		} else {
			account = r.cal.Account.Label
		}
	} else if len(m.calendars) > 0 {
		account = m.calendars[0].Account.Label
	}
	m.form = newAddForm(account)
	return m, m.form.focusField(m.form.focus)
}

func (m Model) handleFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.form
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if !f.checking {
			m.form = nil
		}
		return m, nil
	}
	if f.checking {
		return m, nil
	}
	switch msg.String() {
	case "tab", "down":
		return m, f.focusField(f.focus + 1)
	case "shift+tab", "up":
		return m, f.focusField(f.focus - 1)
	case "enter":
		if f.focus < fieldURL {
			return m, f.focusField(f.focus + 1)
		}
		return m.submitAddForm()
	}
	var cmd tea.Cmd
	f.inputs[f.focus], cmd = f.inputs[f.focus].Update(msg)
	f.err = ""
	return m, cmd
}

// formPaste routes pasted text (URLs usually arrive this way) to the form.
func (m Model) formPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.form.checking {
		return m, nil
	}
	var cmd tea.Cmd
	m.form.inputs[m.form.focus], cmd = m.form.inputs[m.form.focus].Update(msg)
	m.form.err = ""
	return m, cmd
}

func (m Model) submitAddForm() (tea.Model, tea.Cmd) {
	f := m.form
	account := strings.TrimSpace(f.inputs[fieldAccount].Value())
	name := strings.TrimSpace(f.inputs[fieldName].Value())
	rawURL := strings.TrimSpace(f.inputs[fieldURL].Value())
	for i, v := range []string{account, name, rawURL} {
		if v == "" {
			f.err = fieldLabels[i] + " is required."
			return m, f.focusField(i)
		}
	}

	f.checking, f.err = true, ""
	src := m.src
	return m, func() tea.Msg {
		ctx := context.Background()
		feed, err := src.CheckFeed(ctx, rawURL)
		if err != nil {
			return addDoneMsg{err: err}
		}
		cal, err := src.AddCalendar(ctx, account, name, feed)
		return addDoneMsg{cal: cal, events: feed.Events, err: err}
	}
}

func (m Model) addDone(msg addDoneMsg) (tea.Model, tea.Cmd) {
	if m.form == nil {
		return m, nil
	}
	if msg.err != nil {
		m.form.checking = false
		m.form.err = describeAddError(msg.err)
		return m, nil
	}
	m.form = nil
	m.side.sel = sideRow{kind: sideCalendar, cal: msg.cal}.key()
	m.syncing = true
	return m, tea.Batch(m.loadCalendars(), m.syncCalendarCmd(msg.cal))
}

// describeAddError words a failed feed check for the form.
func describeAddError(err error) string {
	var unavailable *domain.CalendarUnavailableError
	if errors.As(err, &unavailable) {
		return "That URL doesn't work (" + unavailable.Reason + "). Check you copied the whole secret address, or that it hasn't been reset."
	}
	return err.Error()
}

func (m Model) syncCalendarCmd(cal app.CalendarInfo) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
		defer cancel()
		o := src.SyncCalendar(ctx, cal)
		return syncDoneMsg{at: time.Now(), results: []app.SyncOutcome{o}}
	}
}

func (m Model) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "Y":
		cal := *m.confirmRemove
		m.confirmRemove = nil
		m.removing = true
		src := m.src
		return m, func() tea.Msg {
			return removeDoneMsg{cal: cal, err: src.RemoveCalendar(context.Background(), cal)}
		}
	case "n", "N", "esc", "q":
		m.confirmRemove = nil
	}
	return m, nil
}

func (m Model) removeDone(msg removeDoneMsg) (tea.Model, tea.Cmd) {
	m.removing = false
	m.actionErr = msg.err
	if msg.err != nil {
		return m, nil
	}
	delete(m.hidden, msg.cal.Calendar.ID)
	cmds := []tea.Cmd{m.loadCalendars(), m.loadEvents()}
	if m.week.loaded {
		cmds = append(cmds, m.loadWeek())
	}
	return m, tea.Batch(cmds...)
}

// Popups.

func (m Model) renderForm() []string {
	f := m.form
	w := min(m.width-4, 64)
	inner := w - 4
	var lines []string
	for i, in := range f.inputs {
		label := dimStyle.Render(fieldLabels[i])
		if i == f.focus {
			label = boldStyle.Render(fieldLabels[i])
		}
		in.SetWidth(inner - 1)
		lines = append(lines, " "+label, " "+in.View(), "")
	}
	switch {
	case f.checking:
		lines = append(lines, " "+busyStyle.Render("Checking the feed…"))
	case f.err != "":
		for _, l := range strings.Split(lipgloss.NewStyle().Width(inner).Render(f.err), "\n") {
			lines = append(lines, " "+errorStyle.Render(l))
		}
	default:
		lines = append(lines, " "+dimStyle.Render("enter next/add · tab move · esc cancel"))
	}
	return panel(w, len(lines)+2, frame{focused: true, tabs: []tab{{label: "Add calendar", active: true}}}, lines)
}

func (m Model) renderConfirm() []string {
	lines := []string{
		" Remove " + m.confirmRemove.Label() + "?",
		" " + dimStyle.Render("Its cached events and stored URL are deleted."),
		"",
		" " + boldStyle.Render("y") + dimStyle.Render(" remove · ") + boldStyle.Render("n") + dimStyle.Render(" cancel"),
	}
	widest := 0
	for _, l := range lines {
		widest = max(widest, ansi.StringWidth(l))
	}
	w := min(m.width-4, widest+3) // border plus a column of padding
	return panel(w, len(lines)+2, frame{focused: true, tabs: []tab{{label: "Remove calendar", active: true}}}, lines)
}
