package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Rows of the dialog, in order.
const (
	rowTime = iota
	rowWeek
	rowSync
	rowPulse
	rowLead
	rowGrace
)

func settingsModel(t *testing.T) (Model, *[]Prefs) {
	t.Helper()
	var saved []Prefs
	m := loaded(t, 110, 34).WithSettings(Settings{
		WeekStart:    time.Monday,
		SyncInterval: 5 * time.Minute,
		PulseLead:    5 * time.Minute,
		PulseGrace:   time.Minute,
		Save:         func(p Prefs) error { saved = append(saved, p); return nil },
	})
	m, _ = send(t, m, press("S"))
	if m.settings == nil {
		t.Fatal("S didn't open the settings")
	}
	return m, &saved
}

// goTo moves the cursor to a row.
func goTo(t *testing.T, m Model, row int) Model {
	t.Helper()
	for m.settings.row != row {
		m, _ = send(t, m, press("j"))
	}
	return m
}

func TestGoldenSettings(t *testing.T) {
	m, _ := settingsModel(t)
	golden(t, "settings", screen(m))
}

func TestActiveChoiceIsMarked(t *testing.T) {
	m, _ := settingsModel(t)
	s := screen(m)
	for _, want := range []string{"[24h]", "[Monday]", "[5m]", "[off]", "[1m]"} {
		if !strings.Contains(s, want) {
			t.Errorf("%s not marked:\n%s", want, s)
		}
	}
	if strings.Contains(s, "[12h]") {
		t.Error("an inactive choice is marked")
	}
}

func TestSettingsOpenWithComma(t *testing.T) {
	m := loaded(t, 100, 30)
	m, _ = send(t, m, press(","))
	if m.settings == nil {
		t.Error(", didn't open the settings")
	}
	m, _ = send(t, m, press("esc"))
	if m.settings != nil {
		t.Error("esc didn't close the settings")
	}
}

func TestTimeFormatCyclesAppliesAndSaves(t *testing.T) {
	m, saved := settingsModel(t)
	m, _ = send(t, m, press("enter"))
	if !m.clock.Twelve || !strings.Contains(screen(m), "[12h]") {
		t.Error("the clock didn't change to 12h")
	}
	if !m.saveCmdRan(t, saved) || !(*saved)[0].Clock12 {
		t.Errorf("saved = %+v", *saved)
	}
	m, _ = send(t, m, press("enter"))
	if m.clock.Twelve {
		t.Error("the clock didn't cycle back to 24h")
	}
	m, _ = send(t, m, press("h"))
	if !m.clock.Twelve {
		t.Error("h didn't step backwards")
	}
}

// saveCmdRan runs the model's save and reports whether it saved anything.
func (m Model) saveCmdRan(t *testing.T, saved *[]Prefs) bool {
	t.Helper()
	before := len(*saved)
	if c := m.saveCmd(); c != nil {
		c()
	}
	return len(*saved) > before
}

func TestWeekStartRegridsTheViews(t *testing.T) {
	m, _ := settingsModel(t)
	m, _ = send(t, m, press("esc"), press("2"))
	m, _ = send(t, m, weekLoadedMsg{start: m.week.start, events: testEvents()})
	if got := m.week.start.In(loc).Weekday(); got != time.Monday {
		t.Fatalf("week starts on %v", got)
	}
	m, _ = send(t, m, press("S"))
	m = goTo(t, m, rowWeek)
	m, cmd := send(t, m, press("enter"))
	if m.weekStart != time.Sunday || cmd == nil {
		t.Fatalf("weekStart = %v, reload = %v", m.weekStart, cmd != nil)
	}
	if got := m.week.start.In(loc).Weekday(); got != time.Sunday {
		t.Errorf("the week view starts on %v", got)
	}
}

func TestSyncIntervalChoicesAndCustomMinutes(t *testing.T) {
	m, saved := settingsModel(t)
	m = goTo(t, m, rowSync)
	for _, want := range []time.Duration{10 * time.Minute, 30 * time.Minute, 0} {
		m, _ = send(t, m, press("enter"))
		if m.poll != want {
			t.Fatalf("poll = %v; want %v", m.poll, want)
		}
	}
	m, _ = send(t, m, press("enter"))
	if m.settings.input == nil {
		t.Fatal("no input for custom minutes")
	}
	m, _ = send(t, m, press("x"), press("4"), press("5"), press("enter"))
	if m.poll != 45*time.Minute || !strings.Contains(screen(m), "[45m]") {
		t.Errorf("poll = %v:\n%s", m.poll, screen(m))
	}
	if !m.saveCmdRan(t, saved) || (*saved)[0].SyncInterval != 45*time.Minute {
		t.Errorf("saved = %+v", *saved)
	}
}

func TestSyncIntervalChangeRestartsTheSchedule(t *testing.T) {
	m, _ := settingsModel(t)
	m = goTo(t, m, rowSync)
	seq := m.pollSeq
	m, _ = send(t, m, press("enter"))
	if m.pollSeq == seq {
		t.Error("the old schedule is still live")
	}
	m, _ = send(t, m, press("enter"), press("enter")) // 30m, then off
	if m.poll != 0 || !m.nextPoll.IsZero() {
		t.Errorf("off left a schedule: poll %v, next %v", m.poll, m.nextPoll)
	}
}

func TestPulseSettingsCycle(t *testing.T) {
	m, saved := settingsModel(t)
	m = goTo(t, m, rowPulse)
	m, _ = send(t, m, press("enter"))
	if !m.pulse.on || !strings.Contains(screen(m), "[on]") {
		t.Fatal("the pulse didn't turn on")
	}
	m = goTo(t, m, rowLead)
	m, _ = send(t, m, press("enter")) // 5m → 1m
	if m.pulse.lead != time.Minute {
		t.Fatalf("lead = %v", m.pulse.lead)
	}
	m, _ = send(t, m, press("enter"), press("9"), press("0"), press("enter"))
	if m.pulse.lead != 90*time.Second || !strings.Contains(screen(m), "[90s]") {
		t.Errorf("lead = %v:\n%s", m.pulse.lead, screen(m))
	}
	m = goTo(t, m, rowGrace)
	m, _ = send(t, m, press("enter")) // 1m → whole event
	if m.pulse.grace != graceEvent {
		t.Errorf("grace = %v", m.pulse.grace)
	}
	if !m.saveCmdRan(t, saved) {
		t.Error("nothing saved")
	}
}

func TestGraceChoices(t *testing.T) {
	m, saved := settingsModel(t)
	m = goTo(t, m, rowGrace)
	m, _ = send(t, m, press("enter")) // 1m → whole event
	if m.pulse.grace != graceEvent || !strings.Contains(screen(m), "[whole event]") {
		t.Fatalf("grace = %v:\n%s", m.pulse.grace, screen(m))
	}
	m, _ = send(t, m, press("enter"), press("7"), press("enter")) // custom 7 minutes
	if m.pulse.grace != 7*time.Minute || !strings.Contains(screen(m), "[7m]") {
		t.Errorf("grace = %v", m.pulse.grace)
	}
	m, _ = send(t, m, press("enter")) // custom → 10m
	if m.pulse.grace != 10*time.Minute {
		t.Errorf("grace = %v; want 10m", m.pulse.grace)
	}
	m.saveCmdRan(t, saved)
	if got := (*saved)[len(*saved)-1].PulseGrace; got != 10*time.Minute {
		t.Errorf("saved grace %v", got)
	}
}

func TestCustomInputIsValidated(t *testing.T) {
	m, _ := settingsModel(t)
	m = goTo(t, m, rowLead)                           // 5m
	m, _ = send(t, m, press("enter"), press("enter")) // 1m, then custom
	m, _ = send(t, m, press("0"), press("enter"))
	if m.settings.input == nil || m.settings.note == "" || m.pulse.lead != time.Minute {
		t.Errorf("0 accepted: lead %v, note %q", m.pulse.lead, m.settings.note)
	}
	m, _ = send(t, m, press("esc"))
	if m.settings.input != nil || m.pulse.lead != time.Minute {
		t.Error("esc didn't cancel the input")
	}
	// Empty goes back to the first choice.
	m, _ = send(t, m, press("enter"), tea.KeyPressMsg{Code: tea.KeyBackspace}, press("enter"))
	if m.pulse.lead != 10*time.Minute {
		t.Errorf("empty entry gave %v; want 10m", m.pulse.lead)
	}
}
