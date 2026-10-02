package ui

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/engine"
)

var update = flag.Bool("update", false, "rewrite golden files")

// The UI never calls the source in these tests: messages are fed to Update
// directly, which is the point of keeping Update pure.
type nopSource struct{}

func (nopSource) Calendars(context.Context) ([]CalendarRow, error) { return nil, nil }
func (nopSource) Events(context.Context, time.Time, time.Time) ([]domain.Event, error) {
	return nil, nil
}
func (nopSource) Sync(context.Context) ([]engine.Result, error) { return nil, nil }

var (
	loc      = time.FixedZone("EEST", 3*3600)
	testNow  = time.Date(2026, 9, 29, 14, 30, 0, 0, loc)
	personal = domain.Account{ID: "p", Provider: "ical", Label: "Personal"}
	mainCal  = domain.Calendar{ID: "main", AccountID: "p", Name: "Main"}
	holidays = domain.Calendar{ID: "hol", AccountID: "p", Name: "Holidays"}
)

func at(day, h, m int) time.Time { return time.Date(2026, 9, day, h, m, 0, 0, loc) }

func testEvents() []domain.Event {
	d := func(y, mo, da int) domain.CivilDate { return domain.NewCivilDate(y, mo, da) }
	return []domain.Event{
		domain.NewAllDayEvent("nameday", "main", "Name day", d(2026, 9, 29), d(2026, 9, 30), "", ""),
		domain.NewTimedEvent("standup", "main", "Standup", at(29, 9, 0), at(29, 9, 30), "", ""),
		domain.NewTimedEvent("dentist", "main", "Dentist", at(29, 15, 0), at(29, 16, 0), "Main St 1",
			"Bring the insurance card. Park in the back, the front lot is closed for repairs until November."),
		domain.NewTimedEvent("train", "main", "Night train", at(29, 23, 0), at(30, 1, 0), "", ""),
		domain.NewAllDayEvent("conf", "main", "Conference", d(2026, 9, 30), d(2026, 10, 2), "", ""),
		domain.NewTimedEvent("reminder", "main", "Reminder", at(30, 12, 0), at(30, 12, 0), "", ""),
		domain.NewAllDayEvent("holiday", "hol", "Holiday", d(2026, 10, 1), d(2026, 10, 2), "", ""),
		domain.NewTimedEvent("jp", "main", "日本語 meeting 🎉", time.Date(2026, 10, 2, 10, 0, 0, 0, loc),
			time.Date(2026, 10, 2, 11, 0, 0, 0, loc), "", ""),
	}
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func send(t *testing.T, m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, msg := range msgs {
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m, cmd
}

// loaded returns a model that has received its window size, calendars,
// events and a finished sync.
func loaded(t *testing.T, width, height int) Model {
	t.Helper()
	m := New(nopSource{}, loc, testNow)
	m.tick = 0
	m, _ = send(t, m,
		tea.WindowSizeMsg{Width: width, Height: height},
		calendarsLoadedMsg{rows: []CalendarRow{{personal, holidays}, {personal, mainCal}}},
		eventsLoadedMsg{from: m.from, days: m.days, events: testEvents()},
		syncDoneMsg{at: at(29, 14, 29)},
	)
	return m
}

func screen(m Model) string {
	return ansi.Strip(m.View().Content)
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch (run with -update to accept)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestGoldenAgenda(t *testing.T) {
	golden(t, "agenda", screen(loaded(t, 100, 24)))
}

func TestGoldenNarrow(t *testing.T) {
	golden(t, "narrow", screen(loaded(t, 56, 16)))
}

func TestGoldenSidebarFocusedAndCollapsed(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("h"))
	golden(t, "sidebar_focused", screen(m))
	m, _ = send(t, m, press("c"))
	golden(t, "sidebar_collapsed", screen(m))
}

func TestGoldenDetail(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("enter"))
	golden(t, "detail", screen(m))
}

func TestEveryLineFitsTheWindow(t *testing.T) {
	for _, size := range [][2]int{{100, 24}, {56, 16}, {30, 6}} {
		m := loaded(t, size[0], size[1])
		for _, m := range []Model{m, func() Model { m, _ := send(t, m, press("enter")); return m }()} {
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d: %d lines, want %d", size[0], size[1], len(lines), size[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%dx%d line %d width %d, want %d: %q", size[0], size[1], i, w, size[0], ansi.Strip(l))
				}
			}
		}
	}
}

func TestDefaultSelectionIsNextEventAfterNow(t *testing.T) {
	m := loaded(t, 100, 24)
	if i := m.selIndex(); i < 0 || m.rows[i].item.Event.ID != "dentist" {
		t.Errorf("selected %v, want dentist (first event after 14:30)", m.rows[max(i, 0)].item.Event.ID)
	}
}

func TestNavigationAndWindowExtension(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("j"))
	if id := m.rows[m.selIndex()].item.Event.ID; id != "train" {
		t.Errorf("after j selected %q, want train", id)
	}
	m, _ = send(t, m, press("k"), press("k"))
	if id := m.rows[m.selIndex()].item.Event.ID; id != "standup" {
		t.Errorf("after k k selected %q, want standup", id)
	}

	m, _ = send(t, m, press("G"))
	daysBefore := m.days
	m, cmd := send(t, m, press("j"))
	if cmd == nil || m.days != daysBefore+extendDays || !m.loading {
		t.Errorf("j at the last event: days %d→%d, loading %v, cmd %v; want an extending load", daysBefore, m.days, m.loading, cmd != nil)
	}
}

func TestStaleEventLoadIsIgnored(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("[")) // steps before the window: from moves to Sep 28 and reloads
	m, _ = send(t, m, eventsLoadedMsg{from: domain.NewCivilDate(2026, 9, 29), days: initialDays, events: nil})
	if !m.loading || len(m.events) == 0 {
		t.Error("a load for an old window was applied; want it ignored")
	}
}

func TestSelectionSurvivesReload(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("j")) // train
	evs := append([]domain.Event{domain.NewTimedEvent("new", "main", "New early event", at(29, 7, 0), at(29, 8, 0), "", "")}, testEvents()...)
	m, _ = send(t, m, eventsLoadedMsg{from: m.from, days: m.days, events: evs})
	if id := m.rows[m.selIndex()].item.Event.ID; id != "train" {
		t.Errorf("after reload selected %q, want train", id)
	}
}

func TestSidebarToggleHidesCalendar(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("h"), press("space")) // cursor starts on Holidays
	for _, r := range m.rows {
		if r.kind == rowItem && r.item.Event.CalendarID == "hol" {
			t.Fatal("Holidays event still shown after hiding the calendar")
		}
	}
	m, _ = send(t, m, press("space"))
	found := false
	for _, r := range m.rows {
		found = found || (r.kind == rowItem && r.item.Event.CalendarID == "hol")
	}
	if !found {
		t.Error("Holidays event missing after showing the calendar again")
	}
}

func TestSyncFailureShownInFooter(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("r"))
	if !strings.Contains(screen(m), "Syncing…") {
		t.Error("footer doesn't show Syncing… after r")
	}
	m, cmd := send(t, m, syncDoneMsg{at: at(29, 14, 31), results: []engine.Result{
		{Account: personal, Calendar: mainCal, Err: &domain.CalendarUnavailableError{CalendarID: "main", Reason: "HTTP 404 Not Found"}},
		{Account: personal, Calendar: holidays, Changed: true, Events: 1},
	}})
	if !strings.Contains(screen(m), "Personal · Main: feed URL no longer works") {
		t.Errorf("footer doesn't show the dead feed:\n%s", screen(m))
	}
	if cmd == nil {
		t.Error("a sync with changes didn't trigger an event reload")
	}
	if m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 32), err: errors.New("boom")}); !strings.Contains(screen(m), "Sync failed: boom") {
		t.Error("footer doesn't show a failed sync")
	}
}

func TestDetailOpensAndCloses(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("enter"))
	if m.detail == nil || m.detail.Event.ID != "dentist" {
		t.Fatal("enter didn't open the selected event")
	}
	m, cmd := send(t, m, press("q"))
	if m.detail != nil || cmd != nil {
		t.Error("q in the detail view should close it, not quit")
	}
	if _, cmd := send(t, m, press("q")); cmd == nil || cmd() != tea.Quit() {
		t.Error("q in the agenda should quit")
	}
}

func TestBuildRowsNowMarker(t *testing.T) {
	m := loaded(t, 100, 24)
	var kinds []string
	for _, r := range m.rows {
		if r.date != m.today() {
			continue
		}
		switch r.kind {
		case rowNow:
			kinds = append(kinds, "NOW")
		case rowItem:
			kinds = append(kinds, r.item.Event.ID)
		}
	}
	want := "nameday standup NOW dentist train"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("today's rows = %q, want %q", got, want)
	}
}

func selected(m Model) string {
	i := m.selIndex()
	if i < 0 {
		return "none"
	}
	r := m.rows[i]
	if r.kind == rowNothing {
		return r.date.String() + " nothing"
	}
	return r.date.String() + " " + r.item.Event.ID
}

// deliver answers the load a command asked for with the test events.
func deliver(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = send(t, m, eventsLoadedMsg{from: m.from, days: m.days, events: testEvents()})
	return m
}

func TestEveryDayIsShown(t *testing.T) {
	m := loaded(t, 100, 24)
	headers := 0
	for _, r := range m.rows {
		if r.kind == rowDay {
			headers++
		}
	}
	if headers != initialDays {
		t.Errorf("%d day headers, want %d (one per day in the window)", headers, initialDays)
	}
}

func TestStepDayFollowsSelection(t *testing.T) {
	m := loaded(t, 100, 24)
	for range 5 {
		m, _ = send(t, m, press("j"))
	}
	if got := selected(m); got != "2026-10-01 conf" {
		t.Fatalf("after 5×j selected %s, want 2026-10-01 conf", got)
	}
	steps := []struct{ key, want string }{
		{"]", "2026-10-02 jp"},      // next day from the selection, not from the agenda start
		{"]", "2026-10-03 nothing"}, // empty days are real stops
		{"[", "2026-10-02 jp"},
		{"[", "2026-10-01 conf"}, // first row of the day
		{"[", "2026-09-30 conf"},
		{"[", "2026-09-29 nameday"},
	}
	for _, s := range steps {
		var cmd tea.Cmd
		m, cmd = send(t, m, press(s.key))
		if cmd != nil {
			t.Fatalf("%s inside the window issued a load", s.key)
		}
		if got := selected(m); got != s.want {
			t.Errorf("after %s selected %s, want %s", s.key, got, s.want)
		}
	}
}

func TestStepDayOutsideWindowLoadsThenSelects(t *testing.T) {
	m := loaded(t, 100, 24)

	m, cmd := send(t, m, press("[")) // Sep 28 is before the window
	if cmd == nil || m.from != domain.NewCivilDate(2026, 9, 28) {
		t.Fatalf("[ before the window: from %s, cmd %v; want from 2026-09-28 and a load", m.from, cmd != nil)
	}
	m = deliver(t, m)
	if got := selected(m); got != "2026-09-28 nothing" {
		t.Errorf("after load selected %s, want 2026-09-28 nothing", got)
	}

	m, _ = send(t, m, press("G"))
	last := m.rows[m.selIndex()].date
	m, cmd = send(t, m, press("]"))
	if cmd == nil {
		t.Fatal("] past the window's end issued no load")
	}
	m = deliver(t, m)
	if got, want := selected(m), last.AddDays(1).String()+" nothing"; got != want {
		t.Errorf("after ] past the end selected %s, want %s", got, want)
	}
}

func TestCurrentDayIsMarked(t *testing.T) {
	m := loaded(t, 100, 24)
	if sc := screen(m); !strings.Contains(sc, "▸ Tue 29 Sep · today") {
		t.Errorf("today not marked as current:\n%s", sc)
	}
	m, _ = send(t, m, press("]"))
	sc := screen(m)
	if !strings.Contains(sc, "▸ Wed 30 Sep") || !strings.Contains(sc, "  Tue 29 Sep · today") {
		t.Errorf("after ] want Wed marked and today unmarked:\n%s", sc)
	}
}

func TestDefaultSelectionStaysOnTodayAfterItsLastEvent(t *testing.T) {
	m := New(nopSource{}, loc, at(29, 23, 30)) // after every event today has started
	m.tick = 0
	m, _ = send(t, m,
		tea.WindowSizeMsg{Width: 100, Height: 24},
		calendarsLoadedMsg{rows: []CalendarRow{{personal, holidays}, {personal, mainCal}}},
		eventsLoadedMsg{from: m.from, days: m.days, events: testEvents()},
	)
	if got := selected(m); got != "2026-09-29 train" {
		t.Errorf("selected %s, want today's last event 2026-09-29 train", got)
	}
}

func TestEnterOnEmptyDayDoesNothing(t *testing.T) {
	m := loaded(t, 100, 24)
	for range 4 {
		m, _ = send(t, m, press("]"))
	}
	if got := selected(m); got != "2026-10-03 nothing" {
		t.Fatalf("selected %s, want 2026-10-03 nothing", got)
	}
	if m, _ = send(t, m, press("enter")); m.detail != nil {
		t.Error("enter on an empty day opened a detail view")
	}
}
