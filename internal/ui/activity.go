package ui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The activity indicator is a fixed slot in the footer's bottom-left corner,
// so there is always one place to look to see whether clical is working.
// It shows, in priority order: a spinner while busy, a brief result after a
// sync, or a quiet "Idle".

const (
	// flashDuration is how long a finished sync's result stays in the slot,
	// so even a sync that takes a fraction of a second is noticed.
	flashDuration = 3 * time.Second
	activityWidth = 14 // fits "✗ Sync failed" plus a space
)

type flashKind int

const (
	flashNone flashKind = iota
	flashOK
	flashFailed
	flashOpened
)

// flashEndMsg clears the flash it was scheduled for; a newer flash (from a
// later sync) has a different until and survives.
type flashEndMsg struct{ until time.Time }

func newSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(busyStyle))
}

// busyLabel names what clical is doing right now, or "" when idle.
func (m Model) busyLabel() string {
	switch {
	case m.form != nil && m.form.checking:
		return "Checking"
	case m.gflow != nil && m.gflow.step == gWaiting && m.gflow.signIn != nil:
		return "Signing in"
	case m.gflow != nil && (m.gflow.step == gChecking || m.gflow.step == gStarting || m.gflow.step == gListing):
		return "Checking"
	case m.gflow != nil && m.gflow.step == gSaving:
		return "Saving"
	case m.removing:
		return "Removing"
	case m.syncing:
		return "Syncing"
	case m.loading || m.week.loading || m.month.loading:
		return "Loading"
	}
	return ""
}

func (m Model) activity() string {
	var s string
	switch {
	case m.busyLabel() != "":
		s = m.spin.View() + " " + busyStyle.Render(m.busyLabel())
	case m.flash == flashOK:
		s = successStyle.Render("✓") + boldStyle.Render(" Synced")
	case m.flash == flashOpened:
		s = accentStyle.Render("↗ Opened")
	case m.flash == flashFailed:
		s = errorStyle.Render("✗") + boldStyle.Render(" Sync failed")
	default:
		s = mutedStyle.Render("◦ Idle")
	}
	return s + strings.Repeat(" ", max(0, activityWidth-ansi.StringWidth(s)))
}

// startFlash shows a sync result and schedules its removal.
func (m *Model) startFlash(kind flashKind, at time.Time) tea.Cmd {
	m.flash = kind
	m.flashUntil = at.Add(flashDuration)
	until := m.flashUntil
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashEndMsg{until: until} })
}

// keepSpinning starts the spinner's tick loop whenever there's work in
// progress and it isn't already running. The loop stops by itself (see
// Update) once nothing is busy.
func (m *Model) keepSpinning() tea.Cmd {
	if m.busyLabel() == "" || m.spinning {
		return nil
	}
	m.spinning = true
	return m.spin.Tick
}
