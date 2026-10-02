package ui

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files")

// The UI never calls the source in these tests: messages are fed to Update
// directly, which is the point of keeping Update pure.
type nopSource struct{}

func (nopSource) Calendars(context.Context) ([]app.CalendarInfo, error) { return nil, nil }
func (nopSource) Events(context.Context, time.Time, time.Time) ([]domain.Event, error) {
	return nil, nil
}
func (nopSource) Sync(context.Context) ([]app.SyncOutcome, error) { return nil, nil }

var (
	loc      = time.FixedZone("EEST", 3*3600)
	testNow  = time.Date(2026, 9, 29, 14, 30, 0, 0, loc)
	personal = domain.Account{ID: "p", Provider: "ical", Label: "Personal"}
	mainCal  = domain.Calendar{ID: "main", AccountID: "p", Name: "Main"}
	holidays = domain.Calendar{ID: "hol", AccountID: "p", Name: "Holidays"}
)

var testCalendars = []app.CalendarInfo{
	{Account: personal, Calendar: holidays},
	{Account: personal, Calendar: mainCal},
}

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
		calendarsLoadedMsg{rows: testCalendars},
		eventsLoadedMsg{from: m.from, days: m.days, events: testEvents()},
		syncDoneMsg{at: at(29, 14, 29)},
		flashEndMsg{until: at(29, 14, 29).Add(flashDuration)},
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
	if !strings.Contains(screen(m), "Syncing") {
		t.Error("footer doesn't show Syncing after r")
	}
	m, cmd := send(t, m, syncDoneMsg{at: at(29, 14, 31), results: []app.SyncOutcome{
		{CalendarInfo: app.CalendarInfo{Account: personal, Calendar: mainCal}, Err: &domain.CalendarUnavailableError{CalendarID: "main", Reason: "HTTP 404 Not Found"}},
		{CalendarInfo: app.CalendarInfo{Account: personal, Calendar: holidays}, Changed: true, Events: 1},
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
		calendarsLoadedMsg{rows: testCalendars},
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

// toWeek switches to the week view and delivers the week load it requested.
func toWeek(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, press("2"))
	return deliverWeek(t, m, events)
}

func deliverWeek(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, weekLoadedMsg{start: m.week.start, events: events})
	return m
}

func weekSelected(m Model) string {
	date := m.selectedWeekDate().String()
	if it, ok := m.selectedWeekItem(); ok {
		return date + " " + it.Event.ID
	}
	return date + " nothing"
}

func TestGoldenWeek(t *testing.T) {
	golden(t, "week", screen(toWeek(t, loaded(t, 100, 24), testEvents())))
	golden(t, "week_narrow", screen(toWeek(t, loaded(t, 56, 16), testEvents())))
	golden(t, "week_too_narrow", screen(toWeek(t, loaded(t, 40, 10), testEvents())))
}

func TestWeekLinesFitTheWindow(t *testing.T) {
	for _, size := range [][2]int{{100, 24}, {56, 16}, {40, 10}, {30, 6}} {
		m := toWeek(t, loaded(t, size[0], size[1]), testEvents())
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines, want %d", size[0], size[1], len(lines), size[1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != size[0] {
				t.Errorf("%dx%d line %d width %d: %q", size[0], size[1], i, w, ansi.Strip(l))
			}
		}
	}
}

func TestSwitchingViewsKeepsTheSelection(t *testing.T) {
	m := loaded(t, 100, 24) // agenda on Dentist, Tue 29 Sep
	m = toWeek(t, m, testEvents())
	if m.week.start != domain.NewCivilDate(2026, 9, 28) || weekSelected(m) != "2026-09-29 dentist" {
		t.Fatalf("week start %s, selected %s; want week of Mon 28 Sep with Dentist", m.week.start, weekSelected(m))
	}

	m, _ = send(t, m, press("l"), press("tab")) // Wed 30, back to the agenda
	if m.view != viewAgenda || selected(m) != "2026-09-30 conf" {
		t.Errorf("after l, tab: view %v, agenda selected %s; want agenda on 2026-09-30 conf", m.view, selected(m))
	}
	m, _ = send(t, m, press("tab"))
	if m.view != viewWeek || weekSelected(m) != "2026-09-30 conf" {
		t.Errorf("tab again: week selected %s, want 2026-09-30 conf (same week, no reload)", weekSelected(m))
	}
}

func TestWeekNavigation(t *testing.T) {
	m := toWeek(t, loaded(t, 100, 24), testEvents())

	m, _ = send(t, m, press("j"))
	if got := weekSelected(m); got != "2026-09-29 train" {
		t.Errorf("j: selected %s, want 2026-09-29 train", got)
	}
	m, _ = send(t, m, press("g"))
	if got := weekSelected(m); got != "2026-09-29 nameday" {
		t.Errorf("g: selected %s, want 2026-09-29 nameday", got)
	}

	m, _ = send(t, m, press("h")) // Mon 28
	if got := weekSelected(m); got != "2026-09-28 nothing" || m.focus != focusMain {
		t.Errorf("h: selected %s focus %v, want Mon 28 with main focus", got, m.focus)
	}
	m, _ = send(t, m, press("h")) // past Monday: into the sidebar
	if m.focus != focusSidebar {
		t.Error("h on Monday didn't focus the sidebar")
	}
	m, _ = send(t, m, press("l"))
	if m.focus != focusMain || m.view != viewWeek {
		t.Error("l from the sidebar didn't return to the week")
	}

	m, cmd := send(t, m, press("]"))
	if cmd == nil || m.week.start != domain.NewCivilDate(2026, 10, 5) {
		t.Fatalf("]: week start %s, cmd %v; want 2026-10-05 and a load", m.week.start, cmd != nil)
	}
	m = deliverWeek(t, m, testEvents())
	if got := weekSelected(m); got != "2026-10-05 nothing" {
		t.Errorf("]: selected %s, want Monday 2026-10-05", got)
	}

	m, _ = send(t, m, press("t"))
	m = deliverWeek(t, m, testEvents())
	if got := weekSelected(m); got != "2026-09-29 dentist" {
		t.Errorf("t: selected %s, want today's next event 2026-09-29 dentist", got)
	}
}

func TestWeekOverflowShowsMoreAndScrollsToSelection(t *testing.T) {
	var events []domain.Event
	for h := 8; h < 20; h++ {
		id := fmt.Sprintf("e%02d", h)
		events = append(events, domain.NewTimedEvent(id, "main", "Event "+id, at(30, h, 0), at(30, h, 30), "", ""))
	}
	m := loaded(t, 100, 16)
	m = toWeek(t, m, events)
	m, _ = send(t, m, press("l")) // Wed 30, 12 events in a short column
	if sc := screen(m); !strings.Contains(sc, "more") {
		t.Fatalf("no \"+N more\" line for an overflowing column:\n%s", sc)
	}
	m, _ = send(t, m, press("G"))
	if sc := screen(m); !strings.Contains(sc, "Event e19") {
		t.Errorf("last event not scrolled into view after G:\n%s", sc)
	}
}

func TestWeekEnterOpensDetail(t *testing.T) {
	m := toWeek(t, loaded(t, 100, 24), testEvents())
	m, _ = send(t, m, press("enter"))
	if m.detail == nil || m.detail.Event.ID != "dentist" {
		t.Fatal("enter in the week view didn't open the selected event")
	}
	m, _ = send(t, m, press("esc"))
	if m.detail != nil || m.view != viewWeek {
		t.Error("esc didn't return to the week view")
	}
}

func TestWeekStaleLoadIgnored(t *testing.T) {
	m := toWeek(t, loaded(t, 100, 24), testEvents())
	m, _ = send(t, m, press("]"))
	m, _ = send(t, m, weekLoadedMsg{start: domain.NewCivilDate(2026, 9, 28), events: nil})
	if !m.week.loading {
		t.Error("a load for the previous week was applied; want it ignored")
	}
}

func activitySlot(m Model) string {
	lines := strings.Split(screen(m), "\n")
	return strings.TrimSpace(ansi.Truncate(lines[len(lines)-1], activityWidth+1, ""))
}

func TestActivityIndicator(t *testing.T) {
	// Startup: Init syncs, so the slot says so from the first frame.
	m := New(nopSource{}, loc, testNow)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	if got := activitySlot(m); !strings.HasSuffix(got, "Syncing") {
		t.Errorf("at startup slot = %q, want a spinner and Syncing", got)
	}

	m = loaded(t, 100, 24)
	if got := activitySlot(m); got != "◦ Idle" {
		t.Errorf("settled slot = %q, want ◦ Idle", got)
	}

	m, cmd := send(t, m, press("r"))
	if got := activitySlot(m); !strings.HasSuffix(got, "Syncing") || cmd == nil {
		t.Errorf("after r slot = %q (cmd %v), want Syncing and a sync command", got, cmd != nil)
	}
	if !m.spinning {
		t.Error("spinner not started while syncing")
	}

	done := at(29, 14, 40)
	m, _ = send(t, m, syncDoneMsg{at: done})
	if got := activitySlot(m); got != "✓ Synced" {
		t.Errorf("after a sync slot = %q, want ✓ Synced", got)
	}

	// A flash-end for an older flash must not clear a newer one.
	m, _ = send(t, m, flashEndMsg{until: done.Add(-time.Minute)})
	if got := activitySlot(m); got != "✓ Synced" {
		t.Errorf("stale flash end cleared the slot: %q", got)
	}
	m, _ = send(t, m, flashEndMsg{until: done.Add(flashDuration)})
	if got := activitySlot(m); got != "◦ Idle" {
		t.Errorf("after the flash slot = %q, want ◦ Idle", got)
	}

	m, _ = send(t, m, press("r"), syncDoneMsg{at: done, err: errors.New("offline")})
	if got := activitySlot(m); got != "✗ Sync failed" {
		t.Errorf("after a failed sync slot = %q, want ✗ Sync failed", got)
	}

	m, _ = send(t, m, press("2")) // week load in flight
	if got := activitySlot(m); !strings.HasSuffix(got, "Loading") {
		t.Errorf("while the week loads slot = %q, want Loading", got)
	}
}

func TestSpinnerStopsWhenIdle(t *testing.T) {
	m := loaded(t, 100, 24)
	m.spinning = true
	next, cmd := m.Update(m.spin.Tick())
	if next.(Model).spinning || cmd != nil {
		t.Error("spinner kept ticking with nothing in progress")
	}
}

func TestFocusedPanelBorderIsHighlighted(t *testing.T) {
	hi, lo := focusedBorderStyle.Render("╭─"), borderStyle.Render("╭─")
	topLine := func(m Model) string { return strings.SplitN(m.View().Content, "\n", 2)[0] }

	m := loaded(t, 100, 24) // main pane focused
	top := topLine(m)
	if !strings.HasPrefix(top, lo) || !strings.Contains(top, hi) {
		t.Errorf("main focused: want a plain sidebar border and a highlighted main border:\n%q", top)
	}
	if !strings.Contains(top, activeTabStyle.Render("[1] Agenda")) || !strings.Contains(top, dimStyle.Render("[2] Week")) {
		t.Errorf("agenda tab not shown as active:\n%q", top)
	}

	m, _ = send(t, m, press("h"))
	top = topLine(m)
	if !strings.HasPrefix(top, hi) || strings.Count(top, hi) != 1 {
		t.Errorf("sidebar focused: want only the sidebar border highlighted:\n%q", top)
	}

	m, _ = send(t, m, press("l"), press("2"))
	m = deliverWeek(t, m, testEvents())
	if top = topLine(m); !strings.Contains(top, activeTabStyle.Render("[2] Week")) {
		t.Errorf("week tab not shown as active:\n%q", top)
	}
}

func TestPanelTopFitsAndDegrades(t *testing.T) {
	tabs := []tab{{label: "[1] Agenda", active: true}, {label: "[2] Week"}}
	for _, w := range []int{100, 40, 26, 12, 5, 2} {
		top := panelTop(w-2, borderStyle, true, tabs, "Week 40 · 28 Sep – 4 Oct 2026")
		if got := ansi.StringWidth(top); got != w {
			t.Errorf("width %d: top line is %d wide: %q", w, got, ansi.Strip(top))
		}
	}
	// With room for the tabs but not the right-hand text, the text goes first.
	top := ansi.Strip(panelTop(30, borderStyle, true, tabs, "Week 40 · 28 Sep – 4 Oct 2026"))
	if !strings.Contains(top, "[2] Week") || strings.Contains(top, "Week 40") {
		t.Errorf("narrow top = %q, want tabs kept and right text dropped", top)
	}
}
