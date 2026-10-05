package ui

import (
	"strings"
	"testing"
	"time"
)

// pulsingModel has the pulse on with a lead long enough that the dentist
// (15:00, 30 minutes after testNow) is pulsing.
func pulsingModel(t *testing.T) Model {
	t.Helper()
	m := loaded(t, 100, 30)
	m.pulse.on = true
	m.pulse.lead, m.pulse.grace = 30*time.Minute, 0
	m, _ = send(t, m, upcomingMsg{events: testEvents()})
	return m
}

func dentist(m Model) bool {
	for _, e := range m.pulsing() {
		if e.ID == "dentist" {
			return true
		}
	}
	return false
}

func TestPulseIsOffByDefault(t *testing.T) {
	m := loaded(t, 100, 30)
	m, _ = send(t, m, upcomingMsg{events: testEvents()})
	if len(m.pulsing()) != 0 || strings.Contains(screen(m), "Dentist in") {
		t.Error("pulsing with the setting off")
	}
}

func TestPulseWindowAndCountdown(t *testing.T) {
	m := pulsingModel(t)
	if !dentist(m) || len(m.pulsing()) != 1 {
		t.Fatalf("pulsing = %v; want only the dentist", m.pulsing())
	}
	if !strings.Contains(screen(m), "● Dentist in 30:00") {
		t.Errorf("no countdown in the status line:\n%s", screen(m))
	}
	m, _ = send(t, m, pulseFrameMsg(at(29, 14, 59).Add(30*time.Second)))
	if !strings.Contains(screen(m), "Dentist in 0:30") {
		t.Errorf("countdown didn't move:\n%s", screen(m))
	}
	m, _ = send(t, m, pulseFrameMsg(at(29, 15, 0)))
	if dentist(m) {
		t.Error("still pulsing at the start time with no grace")
	}
	m.pulse.lead = 5 * time.Minute
	m.pulse.now = time.Time{}
	if dentist(m) {
		t.Error("pulsing 30 minutes ahead with a 5 minute lead")
	}
}

func TestPulseSkipsHiddenCalendarsAndAllDay(t *testing.T) {
	m := pulsingModel(t)
	m.hidden["main"] = true
	if len(m.pulsing()) != 0 {
		t.Error("pulsing an event of a hidden calendar")
	}
	m.hidden["main"] = false
	m.pulse.lead = 24 * time.Hour
	for _, e := range m.pulsing() {
		if e.AllDay {
			t.Errorf("all-day event %q pulses", e.Title)
		}
	}
}

func TestPulseFramesDiffer(t *testing.T) {
	seen := map[string]bool{}
	for f := 0; f < 4; f++ {
		seen[pulseStyle(f).Render("x")] = true
	}
	if len(seen) != 3 {
		t.Errorf("%d distinct frames; want plain, bold and reversed", len(seen))
	}
}

func TestPulseShowsInEveryView(t *testing.T) {
	m := pulsingModel(t)
	for _, view := range []string{"1", "2", "3"} {
		m, _ = send(t, m, press(view))
		if !m.isPulsing(testEvents()[2]) {
			t.Fatal("not pulsing")
		}
		if !strings.Contains(screen(m), "Dentist in") {
			t.Errorf("view %s: no countdown", view)
		}
	}
}

func TestPulseShowsInTheEventViewAndKeyStopsIt(t *testing.T) {
	m := loaded(t, 100, 30)
	m, _ = send(t, m, press("enter")) // the dentist is selected
	if m.detail == nil || m.detail.Event.ID != "dentist" {
		t.Fatal("event view not open on the dentist")
	}
	m.pulse.on, m.pulse.lead = true, 30*time.Minute
	m, _ = send(t, m, upcomingMsg{events: testEvents()})
	if !dentist(m) {
		t.Fatal("the open event doesn't pulse")
	}
	if !strings.Contains(screen(m), "Dentist in") {
		t.Error("no countdown while the event view is open")
	}
	m, _ = send(t, m, press("x")) // any key
	if dentist(m) {
		t.Error("a key in the event view didn't stop the pulse")
	}
}

func TestPulseSurvivesOtherScreens(t *testing.T) {
	m := pulsingModel(t)
	for _, k := range []string{"?", "S"} {
		m, _ = send(t, m, press(k))
		if !strings.Contains(screen(m), "Dentist in") {
			t.Errorf("no countdown after %s", k)
		}
		m, _ = send(t, m, press("esc"))
	}
}

func TestInteractionStopsOnlyThatEvent(t *testing.T) {
	// Other keys, and the preselected event, leave it alone.
	m := pulsingModel(t)
	m, _ = send(t, m, press("0"), press("1"))
	if !dentist(m) {
		t.Fatal("an unrelated key stopped the pulse")
	}
	// Moving off and back onto it is an interaction.
	m, _ = send(t, m, press("j"))
	if !dentist(m) {
		t.Fatal("leaving the event stopped it")
	}
	m, _ = send(t, m, press("k"))
	if dentist(m) {
		t.Error("selecting the event didn't stop it")
	}
	// And it doesn't start again.
	m, _ = send(t, m, press("j"), upcomingMsg{events: testEvents()})
	if dentist(m) {
		t.Error("a stopped event pulses again")
	}
}

func TestOpeningThePulsingEventKeepsItPulsingUntilAKey(t *testing.T) {
	m := pulsingModel(t)
	m, _ = send(t, m, press("enter"))
	if m.detail == nil || !dentist(m) {
		t.Fatal("opening the event stopped its pulse")
	}
	m, _ = send(t, m, press("j"))
	if dentist(m) {
		t.Error("a key in the event view didn't stop it")
	}
}

func TestStartingInsideTheLeadTimePulsesAtOnce(t *testing.T) {
	m := loaded(t, 100, 30) // 14:30; the dentist is at 15:00
	m.pulse.on, m.pulse.lead = true, 10*time.Minute
	m.now = at(29, 14, 52)
	m, cmd := send(t, m, upcomingMsg{events: testEvents()})
	if !dentist(m) || cmd == nil {
		t.Errorf("not pulsing 8 minutes before, with a 10 minute lead (ticks: %v)", cmd != nil)
	}
	m.now = at(29, 14, 49)
	if dentist(m) {
		t.Error("pulsing before the lead time")
	}
}

func TestEventViewLeftOpenStartsPulsingWhenTheLeadIsReached(t *testing.T) {
	m := loaded(t, 100, 30)
	m.pulse.on, m.pulse.lead, m.pulse.grace = true, 10*time.Minute, 0
	m, _ = send(t, m, upcomingMsg{events: testEvents()}, press("enter"))
	if m.detail == nil || dentist(m) {
		t.Fatal("setup: event view open, nothing pulsing yet")
	}
	m, _ = send(t, m, pulseFrameMsg(at(29, 14, 51)))
	if !dentist(m) || !strings.Contains(screen(m), "Dentist in 9:00") {
		t.Errorf("the open event view didn't start pulsing:\n%s", screen(m))
	}
	m, _ = send(t, m, press("j"))
	if dentist(m) {
		t.Error("interacting didn't stop it")
	}
}

func TestPulseGracePeriod(t *testing.T) {
	cases := []struct {
		name  string
		grace time.Duration
		now   time.Time
		want  bool
	}{
		{"just started", 5 * time.Minute, at(29, 15, 0), true},
		{"inside the grace", 5 * time.Minute, at(29, 15, 4), true},
		{"after the grace", 5 * time.Minute, at(29, 15, 6), false},
		{"no grace", 0, at(29, 15, 0), false},
		{"whole event, late", graceEvent, at(29, 15, 59), true},
		{"whole event, over", graceEvent, at(29, 16, 0), false},
		{"grace outlasts the event", 2 * time.Hour, at(29, 16, 1), false},
	}
	for _, c := range cases {
		m := loaded(t, 100, 30)
		m.pulse.on, m.pulse.lead, m.pulse.grace = true, 5*time.Minute, c.grace
		m, _ = send(t, m, upcomingMsg{events: testEvents()}, pulseFrameMsg(c.now))
		if got := dentist(m); got != c.want {
			t.Errorf("%s: pulsing = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestGraceShowsCountUpAndCoversEventsWithoutLength(t *testing.T) {
	m := loaded(t, 100, 30)
	m.pulse.on, m.pulse.lead, m.pulse.grace = true, 5*time.Minute, 5*time.Minute
	m, _ = send(t, m, upcomingMsg{events: testEvents()}, pulseFrameMsg(at(29, 15, 3)))
	if !strings.Contains(screen(m), "Dentist started 3:00 ago") {
		t.Errorf("no count-up:\n%s", screen(m))
	}
	// The reminder has no length; the grace isn't cut short by its end.
	m, _ = send(t, m, pulseFrameMsg(at(30, 12, 4)))
	found := false
	for _, e := range m.pulsing() {
		found = found || e.ID == "reminder"
	}
	if !found {
		t.Error("an event without length doesn't get its grace period")
	}
}

func TestPulseTicksOnlyWhileNeeded(t *testing.T) {
	m := loaded(t, 100, 30)
	m.pulse.on, m.pulse.lead, m.pulse.grace = true, 5*time.Minute, 0
	_, cmd := send(t, m, upcomingMsg{events: testEvents()})
	if cmd != nil {
		t.Error("frame ticks run with nothing about to pulse")
	}
	m.pulse.lead = 30 * time.Minute
	m, cmd = send(t, m, upcomingMsg{events: testEvents()})
	if cmd == nil || !m.pulse.running {
		t.Error("no frame ticks with an event pulsing")
	}
	m, cmd = send(t, m, pulseFrameMsg(at(29, 15, 0)))
	if cmd != nil || m.pulse.running {
		t.Error("frame ticks continue after the start")
	}
}
