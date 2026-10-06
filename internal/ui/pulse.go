package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
)

// The pulse (a setting, off by default) makes an event stand out shortly
// before it starts, and for a grace period after: its line pulses in every
// view, its title pulses in the event view, and the status line counts down
// to it (or up from it) wherever you are. Any interaction with that event
// ends its pulse: selecting it, or a key pressed in its event view.
//
// Colors come from the terminal's palette like everything else, so the pulse
// is a stepped cycle of attributes (plain, bold, reversed, bold) rather than
// a fade, which would need RGB values the theme doesn't give us.
//
// Everything is in this file apart from the calls that draw it: pulseLine
// (agenda, week and month lines, and the event view's title), pulseAlert
// (status line), pulseAfterKey and pulseUpdate (Update). Turning the feature
// off is the setting; removing it is deleting this file and those calls.

const (
	pulseStep = 400 * time.Millisecond // one frame
	// pulseSlack is how early the frame ticks start, so a pulse never begins
	// late for want of a tick. Upcoming events are looked up every clock tick.
	pulseSlack = 31 * time.Second
	// pulseReach is how far past the lead time the cache is searched.
	pulseReach = 2 * time.Minute
)

// graceEvent as a grace period pulses until the event ends.
const graceEvent time.Duration = -1

type pulseState struct {
	on    bool
	lead  time.Duration
	grace time.Duration // after the start; or graceEvent
	// upcoming are events starting soon, from the cache.
	upcoming []domain.Event
	// stopped are the events whose pulse the user ended.
	stopped map[string]bool
	now     time.Time // the last frame tick
	running bool      // frame ticks are in flight
}

type (
	upcomingMsg   struct{ events []domain.Event }
	pulseFrameMsg time.Time
)

func eventKey(e domain.Event) string {
	start, _, _ := e.TimeRange()
	return fmt.Sprintf("%s|%s|%d", e.CalendarID, e.ID, start.Unix())
}

func (m Model) pulseClock() time.Time {
	if m.pulse.now.After(m.now) {
		return m.pulse.now
	}
	return m.now
}

// pulseRefresh looks up the events starting soon.
func (m Model) pulseRefresh() tea.Cmd {
	if !m.pulse.on {
		return nil
	}
	// Back far enough to find an event that started within its grace period.
	src, from := m.src, m.pulseClock().Add(-max(m.pulse.grace, 0))
	to := m.pulseClock().Add(m.pulse.lead + pulseReach)
	return func() tea.Msg {
		events, _ := src.Events(context.Background(), from, to)
		return upcomingMsg{events: events}
	}
}

// pulseEnd is when the event's pulse ends: its grace period after the start,
// or its end, whichever comes first (an event with no length has no end to
// cut the grace short).
func (p pulseState) pulseEnd(e domain.Event) time.Time {
	start, end, _ := e.TimeRange()
	if p.grace == graceEvent {
		if end.Before(start) {
			return start
		}
		return end
	}
	until := start.Add(p.grace)
	if end.After(start) && until.After(end) {
		return end
	}
	return until
}

// windowed lists the timed events of shown calendars whose pulse runs now,
// or runs within early from now, soonest first, leaving out stopped ones.
func (m Model) windowed(early time.Duration) []domain.Event {
	now := m.pulseClock()
	var out []domain.Event
	for _, e := range m.pulse.upcoming {
		start, _, ok := e.TimeRange()
		if !ok || e.AllDay || m.hidden[e.CalendarID] || m.pulse.stopped[eventKey(e)] {
			continue
		}
		if now.Before(start.Add(-m.pulse.lead-early)) || !now.Before(m.pulse.pulseEnd(e)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// pulsing lists the events pulsing now.
func (m Model) pulsing() []domain.Event {
	if !m.pulse.on {
		return nil
	}
	return m.windowed(0)
}

func (m Model) isPulsing(e domain.Event) bool {
	key := eventKey(e)
	for _, p := range m.pulsing() {
		if eventKey(p) == key {
			return true
		}
	}
	return false
}

// pulseArm starts the frame ticks if an event is about to pulse or pulsing.
func (m *Model) pulseArm() tea.Cmd {
	if m.pulse.running || !m.pulse.on || len(m.windowed(pulseSlack)) == 0 {
		return nil
	}
	m.pulse.running = true
	return tea.Tick(pulseStep, func(t time.Time) tea.Msg { return pulseFrameMsg(t) })
}

// pulseUpdate handles the pulse's messages.
func (m Model) pulseUpdate(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case upcomingMsg:
		m.pulse.upcoming = msg.events
		sort.SliceStable(m.pulse.upcoming, func(i, j int) bool {
			a, _, _ := m.pulse.upcoming[i].TimeRange()
			b, _, _ := m.pulse.upcoming[j].TimeRange()
			return a.Before(b)
		})
		return m, m.pulseArm(), true
	case pulseFrameMsg:
		m.pulse.now = time.Time(msg)
		m.pulse.running = false
		return m, m.pulseArm(), true
	}
	return m, nil, false
}

// pulseAfterKey ends the pulse of the event a key press interacted with:
// any key pressed in its event view, or moving the selection onto it.
// Opening the event view doesn't, so an event that pulses keeps doing so
// there. prev is the model before the key.
func (m Model) pulseAfterKey(prev Model) Model {
	if !m.pulse.on {
		return m
	}
	stop := func(e domain.Event) { m.pulse.stopped[eventKey(e)] = true }
	if prev.detail != nil {
		stop(prev.detail.Event)
	}
	if e, ok := m.selectedEvent(); ok {
		if before, had := prev.selectedEvent(); !had || eventKey(before) != eventKey(e) {
			stop(e)
		}
	}
	return m
}

// selectedEvent is the event the cursor is on in the agenda or week view.
func (m Model) selectedEvent() (domain.Event, bool) {
	switch m.view {
	case viewWeek:
		if it, ok := m.selectedWeekItem(); ok {
			return it.Event, true
		}
	case viewAgenda, viewHeat:
		if i := m.selIndex(); i >= 0 && m.rows[i].kind == rowItem {
			return m.rows[i].item.Event, true
		}
	}
	return domain.Event{}, false
}

// pulseStyle is the look of a frame: the accent color, plain, bold, reversed,
// bold.
func pulseStyle(frame int) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(colorAccent)
	switch frame % 4 {
	case 1, 3:
		return s.Bold(true)
	case 2:
		return s.Bold(true).Reverse(true)
	}
	return s
}

func (m Model) pulseFrame() int {
	return int(m.pulseClock().UnixMilli()/pulseStep.Milliseconds()) % 4
}

// pulseLine restyles an event's rendered line while the event is pulsing.
func (m Model) pulseLine(e domain.Event, line string) string {
	if !m.isPulsing(e) {
		return line
	}
	return pulseStyle(m.pulseFrame()).Render(ansi.Strip(line))
}

// pulseAlert is the status line's countdown to the soonest pulsing event, or
// "" when nothing pulses.
func (m Model) pulseAlert() string {
	events := m.pulsing()
	if len(events) == 0 {
		return ""
	}
	start, _, _ := events[0].TimeRange()
	left := max(start.Sub(m.pulseClock()), 0)
	title := ansi.Truncate(strings.ReplaceAll(events[0].Title, "\n", " "), 40, "…")
	when := fmt.Sprintf("in %d:%02d", int(left.Minutes()), int(left.Seconds())%60)
	if !start.After(m.pulseClock()) {
		ago := m.pulseClock().Sub(start)
		when = fmt.Sprintf("started %d:%02d ago", int(ago.Minutes()), int(ago.Seconds())%60)
	}
	text := fmt.Sprintf("● %s %s", title, when)
	if n := len(events) - 1; n > 0 {
		text += fmt.Sprintf(" (+%d)", n)
	}
	return pulseStyle(m.pulseFrame()).Render(text)
}
