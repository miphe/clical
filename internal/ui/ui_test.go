package ui

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Most tests never call the source: messages are fed to Update directly,
// which is the point of keeping Update pure. Tests that run a command to see
// what it asks for use fakeSource's recording and canned answers.
type nopSource struct{}

func (nopSource) Calendars(context.Context) ([]app.CalendarInfo, error) { return nil, nil }
func (nopSource) Events(context.Context, time.Time, time.Time) ([]domain.Event, error) {
	return nil, nil
}
func (nopSource) Sync(context.Context) ([]app.SyncOutcome, error) { return nil, nil }
func (nopSource) SyncCalendar(_ context.Context, c app.CalendarInfo) app.SyncOutcome {
	return app.SyncOutcome{CalendarInfo: c}
}
func (nopSource) CheckFeed(context.Context, string) (app.Feed, error) { return app.Feed{}, nil }
func (nopSource) AddCalendar(context.Context, string, string, app.Feed) (app.CalendarInfo, error) {
	return app.CalendarInfo{}, nil
}
func (nopSource) RemoveCalendar(context.Context, app.CalendarInfo) error { return nil }
func (nopSource) SetCalendarURL(context.Context, app.CalendarInfo, app.Feed) error {
	return nil
}
func (nopSource) RenameCalendar(_ context.Context, c app.CalendarInfo, _ string) (app.CalendarInfo, error) {
	return c, nil
}
func (nopSource) RenameAccount(_ context.Context, a domain.Account, _ string) (domain.Account, error) {
	return a, nil
}
func (nopSource) SavedGoogleClientID() (string, error) { return "", nil }
func (nopSource) BeginGoogleSignIn(string, *app.GoogleClient) (app.SignIn, error) {
	return nil, errors.New("no")
}
func (nopSource) GoogleCalendars(context.Context, domain.Account) ([]app.RemoteCalendar, error) {
	return nil, nil
}
func (nopSource) AddGoogleCalendars(context.Context, domain.Account, []app.RemoteCalendar) ([]app.CalendarInfo, error) {
	return nil, nil
}

type fakeSource struct {
	nopSource
	checkErr  error
	removeErr error
	checked   []string
	added     []string // "account/name"
	removed   []string
}

func (f *fakeSource) CheckFeed(_ context.Context, raw string) (app.Feed, error) {
	f.checked = append(f.checked, raw)
	return app.Feed{Events: 3}, f.checkErr
}

func (f *fakeSource) AddCalendar(_ context.Context, account, name string, _ app.Feed) (app.CalendarInfo, error) {
	f.added = append(f.added, account+"/"+name)
	return app.CalendarInfo{
		Account:  domain.Account{ID: "acct-" + account, Label: account},
		Calendar: domain.Calendar{ID: "new", AccountID: "acct-" + account, Name: name, ColorSlot: 3},
	}, nil
}

func (f *fakeSource) RemoveCalendar(_ context.Context, c app.CalendarInfo) error {
	f.removed = append(f.removed, c.Calendar.ID)
	return f.removeErr
}

var (
	loc      = time.FixedZone("EEST", 3*3600)
	testNow  = time.Date(2026, 9, 29, 14, 30, 0, 0, loc)
	personal = domain.Account{ID: "p", Provider: "ical", Label: "Personal"}
	mainCal  = domain.Calendar{ID: "main", AccountID: "p", Name: "Main", ColorSlot: 1}
	holidays = domain.Calendar{ID: "hol", AccountID: "p", Name: "Holidays", ColorSlot: 2}
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
	m, _ = send(t, m, press("0"))
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
	m, _ = send(t, m, press("0"), press("j"), press("space")) // header, then Holidays
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

	m, _ = send(t, m, press("l"), press("1")) // Wed 30, back to the agenda
	if m.view != viewAgenda || selected(m) != "2026-09-30 conf" {
		t.Errorf("after l, 1: view %v, agenda selected %s; want agenda on 2026-09-30 conf", m.view, selected(m))
	}
	m, _ = send(t, m, press("2"))
	if m.view != viewWeek || weekSelected(m) != "2026-09-30 conf" {
		t.Errorf("2 again: week selected %s, want 2026-09-30 conf (same week, no reload)", weekSelected(m))
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
	m, cmd := send(t, m, press("h")) // past Monday: the previous week's Sunday, not another pane
	if m.focus != focusMain || cmd == nil || m.selectedWeekDate() != domain.NewCivilDate(2026, 9, 27) {
		t.Errorf("h on Monday: focus %v day %s; want main focus on Sun 27 Sep (previous week)", m.focus, m.selectedWeekDate())
	}
	m = deliverWeek(t, m, testEvents())
	m, _ = send(t, m, press("l"))
	m = deliverWeek(t, m, testEvents())
	if m.selectedWeekDate() != domain.NewCivilDate(2026, 9, 28) {
		t.Errorf("l from Sunday: day %s, want Mon 28 Sep (next week)", m.selectedWeekDate())
	}

	m, cmd = send(t, m, press("]"))
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
	if !strings.Contains(top, activeTabStyle.Render("[1] Agenda")) || !strings.Contains(top, mutedStyle.Render("[2] Week")) {
		t.Errorf("agenda tab not shown as active:\n%q", top)
	}

	m, _ = send(t, m, press("0"))
	top = topLine(m)
	if !strings.HasPrefix(top, hi) || strings.Count(top, hi) != 1 {
		t.Errorf("sidebar focused: want only the sidebar border highlighted:\n%q", top)
	}

	m, _ = send(t, m, press("l"), press("2"))
	m = deliverWeek(t, m, testEvents())
	if top = topLine(m); !strings.Contains(top, activeTabStyle.Render("[2] Week 40")) {
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

// manyCalendars returns n calendars split over two accounts.
func manyCalendars(n int) []app.CalendarInfo {
	work := domain.Account{ID: "w", Provider: "ical", Label: "Work"}
	var out []app.CalendarInfo
	for i := 0; i < n; i++ {
		acct := personal
		if i >= n/2 {
			acct = work
		}
		out = append(out, app.CalendarInfo{Account: acct, Calendar: domain.Calendar{
			ID: fmt.Sprintf("c%02d", i), AccountID: acct.ID, Name: fmt.Sprintf("Calendar %02d", i), ColorSlot: i + 1,
		}})
	}
	return out
}

func withCalendars(t *testing.T, m Model, cals []app.CalendarInfo) Model {
	t.Helper()
	m, _ = send(t, m, calendarsLoadedMsg{rows: cals})
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func TestSidebarScrollsToCursor(t *testing.T) {
	m := withCalendars(t, loaded(t, 100, 14), manyCalendars(20))
	m, _ = send(t, m, press("0"), press("G"))
	sc := screen(m)
	if !strings.Contains(sc, "Calendar 19") {
		t.Errorf("last calendar not scrolled into view after G:\n%s", sc)
	}
	if !strings.Contains(sc, "22 of 22") { // 20 calendars + 2 account headers
		t.Errorf("no scroll position in the sidebar title:\n%s", sc)
	}
	m, _ = send(t, m, press("g"))
	if sc := screen(m); !strings.Contains(sc, "Personal (10)") {
		t.Errorf("first row not back in view after g:\n%s", sc)
	}
}

func TestSidebarFoldAndToggleAccount(t *testing.T) {
	m := withCalendars(t, loaded(t, 100, 24), manyCalendars(4))
	m, _ = send(t, m, press("0")) // cursor on the Personal header

	m, _ = send(t, m, press("enter"))
	if sc := screen(m); strings.Contains(sc, "Calendar 00") || !strings.Contains(sc, "▸ Personal (2)") {
		t.Errorf("enter on the header didn't fold Personal:\n%s", sc)
	}
	if r, _ := m.selectedSidebarRow(); r.kind != sideAccount {
		t.Error("cursor left the header after folding")
	}
	m, _ = send(t, m, press("enter"))
	if sc := screen(m); !strings.Contains(sc, "Calendar 00") {
		t.Errorf("enter again didn't unfold Personal:\n%s", sc)
	}

	m, _ = send(t, m, press("space"))
	if !m.hidden["c00"] || !m.hidden["c01"] || m.hidden["c02"] {
		t.Errorf("space on the header hid %v; want both Personal calendars and nothing else", m.hidden)
	}
	m, _ = send(t, m, press("space"))
	if m.hidden["c00"] || m.hidden["c01"] {
		t.Error("space again didn't show Personal's calendars")
	}
}

func TestCalendarBulletsFollowSlotsAndStayDistinct(t *testing.T) {
	// Six ANSI hues times four shapes: every one of the first 24 calendars
	// looks different, without leaving the terminal's palette.
	seen := map[string]int{}
	for slot := 1; slot <= len(calendarHues)*len(calendarShapes); slot++ {
		key := fmt.Sprint(calendarColor(slot)) + calendarGlyph(slot, false)
		if prev, dup := seen[key]; dup {
			t.Errorf("slots %d and %d look the same", prev, slot)
		}
		seen[key] = slot
	}
	if calendarGlyph(1, false) != "●" || calendarGlyph(7, false) != "◆" || calendarGlyph(7, true) != "◇" {
		t.Error("shapes don't progress ● → ◆ (hollow when hidden) after the six hues")
	}
	if calendarColor(7) != calendarColor(1) {
		t.Error("slot 7 should reuse slot 1's hue with a different shape")
	}

	// A calendar's look comes from its slot, not its place in the list.
	m := withCalendars(t, loaded(t, 100, 24), []app.CalendarInfo{
		{Account: personal, Calendar: domain.Calendar{ID: "x", AccountID: "p", Name: "X", ColorSlot: 9}},
	})
	if m.calendarSlot("x") != 9 || !strings.Contains(screen(m), "◆ X") {
		t.Errorf("calendar X (slot 9) should show as ◆ in the sidebar:\n%s", screen(m))
	}
}

func TestAddCalendarFromTheTUI(t *testing.T) {
	src := &fakeSource{}
	m := loaded(t, 100, 24)
	m.src = src

	m, _ = send(t, m, press("a"), press("enter"))
	if m.form == nil || m.form.inputs[fieldAccount].Value() != "Personal" || m.form.focus != fieldName {
		t.Fatal("a didn't open the form with the account filled in and the name focused")
	}
	m = typeText(t, m, "Family q") // q types, it doesn't quit
	m, _ = send(t, m, press("enter"))
	secret := "https://calendar.google.com/calendar/ical/x/private-s3cr3t/basic.ics"
	m, _ = send(t, m, tea.PasteMsg{Content: secret})
	if strings.Contains(screen(m), "s3cr3t") {
		t.Fatal("the secret URL is visible on screen")
	}

	m, cmd := send(t, m, press("enter"))
	if !m.form.checking || cmd == nil || !strings.Contains(screen(m), "Checking") {
		t.Fatal("submitting didn't start the feed check")
	}
	msg := cmd()
	if len(src.checked) != 1 || src.checked[0] != secret || len(src.added) != 1 || src.added[0] != "Personal/Family q" {
		t.Fatalf("checked %v, added %v", src.checked, src.added)
	}

	m, cmd = send(t, m, msg)
	if m.form != nil || !m.syncing || cmd == nil {
		t.Fatal("after adding: want the form closed and the new calendar syncing")
	}
	if m.side.sel != "calendar|new" {
		t.Errorf("sidebar selection = %q, want the new calendar", m.side.sel)
	}
}

func TestAddFormValidatesAndShowsFeedErrors(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("a"), press("enter"), press("enter"), press("enter")) // name → URL → submit
	if m.form.err != "Calendar name is required." || m.form.focus != fieldName {
		t.Errorf("empty name: err %q focus %d", m.form.err, m.form.focus)
	}

	m = typeText(t, m, "Main")
	m, _ = send(t, m, press("enter"))
	m = typeText(t, m, "https://x.test/a.ics")
	m, _ = send(t, m, press("enter"),
		addDoneMsg{err: &domain.CalendarUnavailableError{Reason: "HTTP 404 Not Found"}})
	if m.form == nil || m.form.checking {
		t.Fatal("a failed check closed the form or left it checking")
	}
	if sc := screen(m); !strings.Contains(sc, "That URL doesn't work (HTTP 404 Not Found)") {
		t.Errorf("feed error not shown in the form:\n%s", sc)
	}

	m, _ = send(t, m, press("esc"))
	if m.form != nil {
		t.Error("esc didn't close the form")
	}
}

func TestRemoveCalendarFromTheTUI(t *testing.T) {
	src := &fakeSource{}
	m := loaded(t, 100, 24)
	m.src = src
	m, _ = send(t, m, press("0"), press("j"), press("d")) // Holidays
	if m.confirmRemove == nil || !strings.Contains(screen(m), "Remove Personal · Holidays?") {
		t.Fatalf("d didn't ask to remove Holidays:\n%s", screen(m))
	}
	m, _ = send(t, m, press("n"))
	if m.confirmRemove != nil || len(src.removed) != 0 {
		t.Fatal("n didn't cancel")
	}

	m, _ = send(t, m, press("d"))
	m, cmd := send(t, m, press("y"))
	if cmd == nil || !strings.Contains(screen(m), "Removing") {
		t.Fatal("y didn't start removing")
	}
	m, cmd = send(t, m, cmd())
	if len(src.removed) != 1 || src.removed[0] != "hol" || cmd == nil {
		t.Errorf("removed %v (reload cmd %v); want hol and a reload", src.removed, cmd != nil)
	}

	src.removeErr = errors.New("disk full")
	m, _ = send(t, m, press("d"), press("y"))
	m, _ = send(t, m, removeDoneMsg{err: src.removeErr})
	if !strings.Contains(screen(m), "disk full") {
		t.Error("a failed removal isn't reported")
	}
}

func TestEmptyStateInvitesAdding(t *testing.T) {
	m := New(nopSource{}, loc, testNow)
	m.tick = 0
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 24}, calendarsLoadedMsg{rows: nil})
	sc := screen(m)
	if !strings.Contains(sc, "No calendars yet.") || !strings.Contains(sc, "a add one") {
		t.Errorf("empty sidebar doesn't explain how to add a calendar:\n%s", sc)
	}
	if m, _ = send(t, m, press("a"), press("enter")); m.form == nil || m.form.focus != fieldAccount {
		t.Error("a with no calendars should open the form on the account field")
	}
}

func TestPopupsFitTheWindow(t *testing.T) {
	for _, size := range [][2]int{{100, 24}, {56, 16}, {30, 12}} {
		m := loaded(t, size[0], size[1])
		form, _ := send(t, m, press("a"), press("enter"))
		confirm, _ := send(t, m, press("0"), press("j"), press("d"))
		for name, mm := range map[string]Model{"form": form, "confirm": confirm} {
			for i, l := range strings.Split(mm.View().Content, "\n") {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%s %dx%d line %d width %d: %q", name, size[0], size[1], i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestGoldenPopups(t *testing.T) {
	m := loaded(t, 100, 24)
	form, _ := send(t, m, press("a"), press("enter"))
	golden(t, "add_form", screen(form))
	confirm, _ := send(t, m, press("0"), press("j"), press("d"))
	golden(t, "remove_confirm", screen(confirm))
}

// sgrParams returns the parameters of every SGR (style) escape in s.
func sgrParams(s string) [][]int {
	var out [][]int
	for _, m := range regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindAllStringSubmatch(s, -1) {
		var params []int
		for _, p := range strings.Split(m[1], ";") {
			n, _ := strconv.Atoi(p)
			params = append(params, n)
		}
		out = append(out, params)
	}
	return out
}

func renderWithProfile(t *testing.T, m Model, env []string) string {
	t.Helper()
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.Env(env)}
	if _, err := w.WriteString(m.View().Content); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func screensToCheck(t *testing.T) map[string]Model {
	m := loaded(t, 100, 24)
	week := toWeek(t, m, testEvents())
	side, _ := send(t, m, press("0"))
	form, _ := send(t, m, press("a"), press("enter"))
	syncing, _ := send(t, m, press("r"))
	month := toMonth(t, m, testEvents())
	return map[string]Model{"agenda": m, "week": week, "month": month, "sidebar": side, "form": form, "syncing": syncing}
}

func TestOnlyANSIColorsReachTheTerminal(t *testing.T) {
	// Even on a truecolor terminal, nothing is upgraded: every color is a
	// basic ANSI foreground (30–37, 90–97), never 256-color or RGB (38;…).
	env := []string{"TERM=xterm-256color", "COLORTERM=truecolor"}
	for name, m := range screensToCheck(t) {
		for _, params := range sgrParams(renderWithProfile(t, m, env)) {
			for i, p := range params {
				switch {
				case p == 38 || p == 48:
					t.Errorf("%s: extended color escape %v", name, params[i:])
				case p >= 40 && p <= 47, p >= 100 && p <= 107:
					t.Errorf("%s: background color set (%d); the terminal's own background must show", name, p)
				}
			}
		}
	}
}

func TestNoColorKeepsStatesVisible(t *testing.T) {
	env := []string{"TERM=xterm-256color", "NO_COLOR=1"}
	for name, m := range screensToCheck(t) {
		has := map[int]bool{}
		for _, params := range sgrParams(renderWithProfile(t, m, env)) {
			for _, p := range params {
				if (p >= 30 && p <= 37) || (p >= 90 && p <= 97) || p == 38 {
					t.Errorf("%s: color %d emitted under NO_COLOR", name, p)
				}
				has[p] = true
			}
		}
		// Selection (reverse) or, in the sidebar, the current-day marker
		// (underline) must survive without color, and so must bold.
		if !has[1] {
			t.Errorf("%s: no bold left under NO_COLOR", name)
		}
		if name != "form" && !has[7] && !has[4] {
			t.Errorf("%s: selection/current day not visible without color", name)
		}
	}
}

func TestWeekTabShowsTheWeekNumber(t *testing.T) {
	m := loaded(t, 100, 24)
	if top := ansi.Strip(strings.SplitN(screen(m), "\n", 2)[0]); !strings.Contains(top, "[2] Week ─") {
		t.Errorf("agenda view: week tab should be plain \"[2] Week\":\n%s", top)
	}

	m = toWeek(t, m, testEvents())
	top := ansi.Strip(strings.SplitN(screen(m), "\n", 2)[0])
	if !strings.Contains(top, "[2] Week 40") || !strings.Contains(top, "28 Sep – 4 Oct 2026") || strings.Count(top, "40") != 1 {
		t.Errorf("week view top edge = %q; want the tab \"[2] Week 40\" and the dates, number shown once", top)
	}

	// Across a year boundary ISO numbering wraps: the week of 28 Dec 2026 is 53,
	// the week of 4 Jan 2027 is 1.
	m, _ = send(t, m, weekLoadedMsg{}, press("t"))
	m.week.start = domain.NewCivilDate(2026, 12, 28)
	if m.weekNumber() != 53 {
		t.Errorf("week of 28 Dec 2026 = %d, want 53", m.weekNumber())
	}
	m.week.start = domain.NewCivilDate(2027, 1, 4)
	if m.weekNumber() != 1 {
		t.Errorf("week of 4 Jan 2027 = %d, want 1", m.weekNumber())
	}
}

// toMonth switches to the month view and delivers the load it requested.
func toMonth(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, press("3"))
	return deliverMonth(t, m, events)
}

func deliverMonth(t *testing.T, m Model, events []domain.Event) Model {
	t.Helper()
	m, _ = send(t, m, monthLoadedMsg{start: m.month.start, weeks: m.month.weeks, events: events})
	return m
}

func TestGoldenMonth(t *testing.T) {
	golden(t, "month", screen(toMonth(t, loaded(t, 100, 30), testEvents())))
	golden(t, "month_short", screen(toMonth(t, loaded(t, 100, 18), testEvents())))
	golden(t, "month_narrow", screen(toMonth(t, loaded(t, 56, 20), testEvents())))
	golden(t, "month_too_narrow", screen(toMonth(t, loaded(t, 34, 12), testEvents())))
}

func TestMonthLinesFitTheWindow(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {100, 18}, {56, 20}, {34, 12}, {30, 6}} {
		m := toMonth(t, loaded(t, size[0], size[1]), testEvents())
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

func TestMonthGridAndHeader(t *testing.T) {
	m := toMonth(t, loaded(t, 100, 30), testEvents())
	if m.month.month != domain.NewCivilDate(2026, 9, 1) || m.month.start != domain.NewCivilDate(2026, 8, 31) || m.month.weeks != 5 {
		t.Fatalf("month %s grid %s×%d, want September 2026 from Mon 31 Aug, 5 weeks", m.month.month, m.month.start, m.month.weeks)
	}
	sc := screen(m)
	for _, want := range []string{"[3] September 2026", "Weeks 36–40", "Mon", "Sun", "36", "40", "Name day", "+2 more"} {
		if !strings.Contains(sc, want) {
			t.Errorf("month screen lacks %q:\n%s", want, sc)
		}
	}
}

func TestMonthNavigation(t *testing.T) {
	m := toMonth(t, loaded(t, 100, 30), testEvents()) // Tue 29 Sep selected
	if m.month.sel != domain.NewCivilDate(2026, 9, 29) {
		t.Fatalf("selected %s, want today 2026-09-29", m.month.sel)
	}
	steps := []struct {
		key  string
		want domain.CivilDate
	}{
		{"l", domain.NewCivilDate(2026, 9, 30)},
		{"k", domain.NewCivilDate(2026, 9, 23)},
		{"g", domain.NewCivilDate(2026, 9, 1)},
		{"G", domain.NewCivilDate(2026, 9, 30)},
		{"[", domain.NewCivilDate(2026, 8, 30)}, // month back, day clamped... 30 Aug exists
	}
	for _, s := range steps {
		m, _ = send(t, m, press(s.key))
		if s.key == "[" {
			m = deliverMonth(t, m, testEvents())
		}
		if m.month.sel != s.want {
			t.Errorf("after %s selected %s, want %s", s.key, m.month.sel, s.want)
		}
	}
	if m.month.month != domain.NewCivilDate(2026, 8, 1) {
		t.Errorf("after [ the shown month is %s, want August", m.month.month)
	}

	// Moving into the next month loads it.
	m, _ = send(t, m, press("t"))
	m = deliverMonth(t, m, testEvents())
	m, cmd := send(t, m, press("j"), press("j")) // 29 Sep → 6 Oct → 13 Oct
	_ = cmd
	if m.month.month != domain.NewCivilDate(2026, 10, 1) || !m.month.loading {
		t.Errorf("j into October: shown %s, loading %v; want October loading", m.month.month, m.month.loading)
	}

	// h on a Monday is just the previous day; it never changes pane.
	m = deliverMonth(t, m, testEvents())
	m, _ = send(t, m, press("g")) // 1 Oct, a Thursday
	for m.month.sel.Weekday() != time.Monday {
		m, _ = send(t, m, press("h"))
		m = deliverMonth(t, m, testEvents())
	}
	monday := m.month.sel
	m, _ = send(t, m, press("h"))
	m = deliverMonth(t, m, testEvents())
	if m.focus != focusMain || m.month.sel != monday.AddDays(-1) {
		t.Errorf("h on a Monday: focus %v, day %s; want main focus on %s", m.focus, m.month.sel, monday.AddDays(-1))
	}
}

func TestMonthEnterOpensTheDayInTheAgenda(t *testing.T) {
	m := toMonth(t, loaded(t, 100, 30), testEvents())
	m, _ = send(t, m, press("l"), press("enter")) // Wed 30 Sep
	if m.view != viewAgenda || m.currentDay() != domain.NewCivilDate(2026, 9, 30) {
		t.Errorf("enter: view %v, agenda day %s; want the agenda on 2026-09-30", m.view, m.currentDay())
	}
}

func TestNumberKeysSwitchViewsKeepingTheDay(t *testing.T) {
	m := loaded(t, 100, 30)
	m, _ = send(t, m, press("]")) // agenda on Wed 30 Sep
	m, _ = send(t, m, press("2"))
	m = deliverWeek(t, m, testEvents())
	if m.view != viewWeek || m.selectedWeekDate() != domain.NewCivilDate(2026, 9, 30) {
		t.Fatalf("2: view %v day %s", m.view, m.selectedWeekDate())
	}
	m, _ = send(t, m, press("3"))
	m = deliverMonth(t, m, testEvents())
	if m.view != viewMonth || m.month.sel != domain.NewCivilDate(2026, 9, 30) {
		t.Fatalf("3: view %v day %s", m.view, m.month.sel)
	}
	m, _ = send(t, m, press("1"))
	if m.view != viewAgenda || m.currentDay() != domain.NewCivilDate(2026, 9, 30) {
		t.Fatalf("1: view %v day %s", m.view, m.currentDay())
	}
}

// The promise: from any view and position, the same keys reach each pane,
// and keys inside a pane never move focus to another one.
func TestPaneKeysWorkFromEverywhere(t *testing.T) {
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	starts := map[string]func() Model{
		"agenda":   func() Model { return loaded(t, 100, 30) },
		"week Tue": func() Model { return toWeek(t, loaded(t, 100, 30), testEvents()) },
		"week Mon": func() Model {
			m := toWeek(t, loaded(t, 100, 30), testEvents())
			m, _ = send(t, m, press("h"))
			return m
		},
		"month Tue": func() Model { return toMonth(t, loaded(t, 100, 30), testEvents()) },
		"month Mon": func() Model {
			m := toMonth(t, loaded(t, 100, 30), testEvents())
			m, _ = send(t, m, press("h"))
			return deliverMonth(t, m, testEvents())
		},
		"list, folded": func() Model { m, _ := send(t, loaded(t, 100, 30), press("0"), press("enter")); return m },
	}
	for name, start := range starts {
		if m, _ := send(t, start(), press("0")); m.focus != focusSidebar {
			t.Errorf("%s: 0 didn't focus the calendar list", name)
		}
		for k, view := range map[string]viewKind{"1": viewAgenda, "2": viewWeek, "3": viewMonth} {
			m, _ := send(t, start(), press("0"), press(k))
			if m.focus != focusMain || m.view != view {
				t.Errorf("%s: %s gave focus %v view %v, want the main pane on view %v", name, k, m.focus, m.view, view)
			}
		}
		m := start()
		before := m.focus
		m, _ = send(t, m, tab)
		if m.focus == before {
			t.Errorf("%s: tab didn't switch pane", name)
		}
		if m, _ = send(t, m, tab); m.focus != before {
			t.Errorf("%s: tab twice didn't come back", name)
		}
		if start().focus == focusMain {
			for _, k := range []string{"h", "l"} {
				if m, _ := send(t, start(), press(k)); m.focus != focusMain {
					t.Errorf("%s: %s moved focus out of the main pane", name, k)
				}
			}
		}
	}
}

func TestFooterAlwaysStartsWithPaneKeys(t *testing.T) {
	for name, m := range map[string]Model{
		"agenda": loaded(t, 100, 30),
		"week":   toWeek(t, loaded(t, 100, 30), testEvents()),
		"month":  toMonth(t, loaded(t, 100, 30), testEvents()),
		"list":   func() Model { m, _ := send(t, loaded(t, 100, 30), press("0")); return m }(),
	} {
		lines := strings.Split(screen(m), "\n")
		if hints := strings.TrimSpace(lines[len(lines)-2]); !strings.HasPrefix(hints, "0 calendars · 1-3 views · tab switch pane │") {
			t.Errorf("%s footer = %q", name, hints)
		}
	}
	if !strings.Contains(screen(loaded(t, 100, 30)), "[0] Calendars") {
		t.Error("calendar list title doesn't show its key")
	}
}

func TestEnterInTheListOnlyFolds(t *testing.T) {
	m := loaded(t, 100, 30)
	m, _ = send(t, m, press("0"), press("j"), press("enter")) // on the Holidays calendar row
	if m.hidden["hol"] {
		t.Error("enter on a calendar row toggled it; only space should")
	}
}

func TestMonthOverflowShowsMore(t *testing.T) {
	var events []domain.Event
	for h := 8; h < 16; h++ {
		id := fmt.Sprintf("m%02d", h)
		events = append(events, domain.NewTimedEvent(id, "main", "Busy "+id, at(30, h, 0), at(30, h, 30), "", ""))
	}
	m := toMonth(t, loaded(t, 100, 24), events)
	if sc := screen(m); !strings.Contains(sc, "more") {
		t.Errorf("no \"+N more\" in an overflowing day:\n%s", sc)
	}
}

func TestMonthStaleLoadIgnored(t *testing.T) {
	m := toMonth(t, loaded(t, 100, 30), testEvents())
	m, _ = send(t, m, press("]"))
	m, _ = send(t, m, monthLoadedMsg{start: domain.NewCivilDate(2026, 8, 31), weeks: 5})
	if !m.month.loading {
		t.Error("a load for the previous month was applied; want it ignored")
	}
}

func TestMoreLabel(t *testing.T) {
	cases := []struct {
		n     int
		none  bool
		width int
		want  string
	}{
		{3, false, 10, "+3 more"},
		{3, false, 5, "+3"},
		{4, true, 10, "4 events"},
		{4, true, 5, "4"},
	}
	for _, c := range cases {
		if got := moreLabel(c.n, c.none, c.width); got != c.want {
			t.Errorf("moreLabel(%d, %v, %d) = %q, want %q", c.n, c.none, c.width, got, c.want)
		}
	}
}

const longURL = "https://docs.google.com/document/d/1aBcD3fGhIjKlMnOpQrStUvWxYz0123456789abcdefghijklmnopqrstuvwxyzABCDEFG/edit?usp=sharing&ouid=112233445566778899000&rtpof=true&sd=true&tracking=" +
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa#heading=h.xyz"

func linkEvent() domain.Event {
	return domain.NewTimedEvent("sync", "main", "Weekly sync", at(29, 15, 0), at(29, 15, 30), "",
		"Agenda in the Agenda doc [2]. Recording notes in [3].").WithLinks([]domain.Link{
		{URL: "https://meet.google.com/abc-defg-hij", Label: "Join Google Meet", Kind: domain.LinkVideoCall},
		{URL: "https://docs.google.com/document/d/abc/edit", Label: "Agenda doc", Kind: domain.LinkDocument},
		{URL: longURL, Label: "Recording notes", Kind: domain.LinkDocument},
	})
}

// detailOn opens the event view for ev in a w×h window.
func detailOn(t *testing.T, w, h int, ev domain.Event) Model {
	t.Helper()
	m := loaded(t, w, h)
	m, _ = send(t, m, eventsLoadedMsg{from: m.from, days: m.days, events: []domain.Event{ev}})
	m, _ = send(t, m, press("enter"))
	if m.detail == nil || m.detail.Event.ID != ev.ID {
		t.Fatalf("enter didn't open %s", ev.ID)
	}
	return m
}

// stripText is the plain text of the "Opens" strip below the rule.
func stripText(m Model) string {
	lines := strings.Split(screen(m), "\n")
	var out []string
	in := false
	for _, l := range lines {
		plain := strings.Trim(ansi.Strip(l), "│ ")
		if strings.HasPrefix(plain, "─ Opens") {
			in = true
			continue
		}
		if in {
			if strings.HasPrefix(plain, "╰") || strings.HasPrefix(ansi.Strip(l), "╰") {
				break
			}
			out = append(out, plain)
		}
	}
	return strings.Join(out, "")
}

func TestGoldenDetailWithLinks(t *testing.T) {
	m := detailOn(t, 90, 24, linkEvent())
	golden(t, "detail_links", screen(m))
	m, _ = send(t, m, press("3"))
	golden(t, "detail_long_link", screen(m))
}

func TestFullAddressIsNeverTruncated(t *testing.T) {
	for _, w := range []int{120, 80, 50, 34} {
		m := detailOn(t, w, 40, linkEvent())
		m, _ = send(t, m, press("3"))
		if got := stripText(m); got != longURL {
			t.Errorf("width %d: strip shows %q, want the whole URL", w, got)
		}
		for i, l := range strings.Split(m.View().Content, "\n") {
			if lw := ansi.StringWidth(l); lw != w {
				t.Errorf("width %d line %d is %d wide", w, i, lw)
			}
		}
	}
}

func TestLinkCanOnlyOpenWhenFullyVisible(t *testing.T) {
	var opened []string
	m := detailOn(t, 40, 12, linkEvent()) // the long URL can't fit here
	m.openURL = func(u string) error { opened = append(opened, u); return nil }
	m, _ = send(t, m, press("3"))
	if !strings.Contains(screen(m), "Window too small") {
		t.Errorf("no notice that the address doesn't fit:\n%s", screen(m))
	}
	m, cmd := send(t, m, press("enter"))
	if cmd != nil || len(opened) != 0 || m.actionErr == nil {
		t.Error("a link opened without its full address on screen")
	}
}

func TestSelectAndOpenLinks(t *testing.T) {
	var opened []string
	m := detailOn(t, 100, 30, linkEvent())
	m.openURL = func(u string) error { opened = append(opened, u); return nil }

	if stripText(m) != "https://meet.google.com/abc-defg-hij" {
		t.Errorf("video call isn't selected first: strip %q", stripText(m))
	}
	m, _ = send(t, m, press("j"))
	if stripText(m) != "https://docs.google.com/document/d/abc/edit" {
		t.Errorf("j: strip %q", stripText(m))
	}
	m, _ = send(t, m, press("1"), press("2"))
	if m.detail.link != 1 {
		t.Errorf("2 selected link %d, want index 1", m.detail.link)
	}

	m, cmd := send(t, m, press("enter"))
	if cmd == nil || m.detail == nil {
		t.Fatal("enter didn't start opening (or closed the view)")
	}
	m, _ = send(t, m, cmd())
	if len(opened) != 1 || opened[0] != "https://docs.google.com/document/d/abc/edit" {
		t.Errorf("opened %v, want the selected doc only", opened)
	}
	if got := activitySlot(m); got != "↗ Opened" {
		t.Errorf("activity after opening = %q", got)
	}

	m, _ = send(t, m, openedMsg{err: errors.New("no browser found")})
	if !strings.Contains(screen(m), "no browser found") {
		t.Error("a failure to open isn't reported")
	}
}

func TestOnlyWebLinksOpen(t *testing.T) {
	var opened []string
	ev := domain.NewTimedEvent("x", "main", "X", at(29, 15, 0), at(29, 16, 0), "", "").WithLinks([]domain.Link{
		{URL: "javascript:alert(1)", Label: "Click me"},
	})
	m := detailOn(t, 100, 30, ev)
	m.openURL = func(u string) error { opened = append(opened, u); return nil }
	m, cmd := send(t, m, press("enter"))
	if cmd != nil || len(opened) != 0 || m.actionErr == nil {
		t.Error("a non-web link was opened")
	}
	if err := openInBrowser("file:///etc/passwd"); err == nil {
		t.Error("openInBrowser accepted a file: URL")
	}
}

func TestAddressShowsHostInBoldAndIsClickable(t *testing.T) {
	m := detailOn(t, 100, 30, linkEvent())
	raw := m.View().Content
	if !strings.Contains(raw, boldStyle.Render("meet.google.com")) {
		t.Error("host isn't bold in the address strip")
	}
	if !strings.Contains(raw, ansi.SetHyperlink("https://meet.google.com/abc-defg-hij")) {
		t.Error("address isn't a terminal hyperlink")
	}
}

func TestDetailScrollsLongNotes(t *testing.T) {
	notes := strings.Repeat("A long line of notes that wraps. ", 60)
	ev := linkEvent()
	ev.Notes = notes
	m := detailOn(t, 80, 20, ev)
	if !strings.Contains(screen(m), "lines 1–") {
		t.Fatalf("no scroll position for long notes:\n%s", screen(m))
	}
	before := screen(m)
	m, _ = send(t, m, press("pgdown"))
	if screen(m) == before || m.detail.scroll == 0 {
		t.Error("pgdown didn't scroll")
	}
	// The address strip stays put while the body scrolls.
	if stripText(m) != "https://meet.google.com/abc-defg-hij" {
		t.Errorf("strip changed while scrolling: %q", stripText(m))
	}
	m, _ = send(t, m, press("3")) // selecting a link scrolls it back into view
	if !strings.Contains(screen(m), "▸ 3") {
		t.Errorf("selected link row not scrolled into view:\n%s", screen(m))
	}
}

// Google flow.

type fakeSignIn struct {
	url       string
	acct      domain.Account
	err       error
	cancelled bool
	onSuccess func()
}

func (f *fakeSignIn) URL() string { return f.url }
func (f *fakeSignIn) Wait(context.Context) (domain.Account, error) {
	if f.err == nil && f.onSuccess != nil {
		f.onSuccess()
	}
	return f.acct, f.err
}
func (f *fakeSignIn) Cancel() { f.cancelled = true }

// googleSource fakes the app's Google side, including its rule that a
// typed-in client is saved only when a sign-in with it succeeds.
type googleSource struct {
	nopSource
	savedID string
	expects []string
	clients []*app.GoogleClient // the typed-in client each sign-in got (nil = saved one)
	signIn  *fakeSignIn
	cals    []app.RemoteCalendar
	added   []app.RemoteCalendar
}

func (g *googleSource) SavedGoogleClientID() (string, error) { return g.savedID, nil }
func (g *googleSource) BeginGoogleSignIn(expect string, client *app.GoogleClient) (app.SignIn, error) {
	g.expects = append(g.expects, expect)
	g.clients = append(g.clients, client)
	g.signIn.onSuccess = func() {
		if client != nil {
			g.savedID = client.ID
		}
	}
	return g.signIn, nil
}
func (g *googleSource) GoogleCalendars(context.Context, domain.Account) ([]app.RemoteCalendar, error) {
	return g.cals, nil
}
func (g *googleSource) AddGoogleCalendars(_ context.Context, acct domain.Account, picks []app.RemoteCalendar) ([]app.CalendarInfo, error) {
	g.added = append(g.added, picks...)
	var out []app.CalendarInfo
	for _, p := range picks {
		out = append(out, app.CalendarInfo{Account: acct, Calendar: domain.Calendar{ID: acct.ID + "/" + p.ID, AccountID: acct.ID, Name: p.Name}})
	}
	return out, nil
}

// run executes cmd (and any batch it returns), feeding back every message
// that arrives within a moment; timers (spinner, flash) are left to expire.
func run(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	for _, msg := range quickMsgs(cmd) {
		var next tea.Cmd
		m, next = send(t, m, msg)
		if next != nil {
			m, _ = run(t, m, next)
		}
	}
	return m, nil
}

func quickMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, quickMsgs(c)...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		if _, ok := msg.(spinnerTick); ok {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(150 * time.Millisecond):
		return nil
	}
}

type spinnerTick = interface{ ID() int }

const signInURL = "https://accounts.google.com/o/oauth2/auth?client_id=cid.apps.googleusercontent.com&redirect_uri=http%3A%2F%2F127.0.0.1%3A49152%2Fcallback&response_type=code&scope=openid+email+https%3A%2F%2Fwww.googleapis.com%2Fauth%2Fcalendar.readonly&state=abc&code_challenge=xyz&code_challenge_method=S256&access_type=offline"

func TestGoogleFlowFirstTime(t *testing.T) {
	work := domain.Account{ID: "g1", Provider: "google", Label: "me@company.com"}
	src := &googleSource{
		signIn: &fakeSignIn{url: signInURL, acct: work},
		cals: []app.RemoteCalendar{
			{ID: "me@company.com", Name: "Me", Primary: true},
			{ID: "team@group", Name: "Team"},
			{ID: "old@group", Name: "Old", Added: true},
		},
	}
	var opened []string
	m := loaded(t, 100, 30)
	m.src = src
	m.openURL = func(u string) error { opened = append(opened, u); return nil }

	m, _ = send(t, m, press("a"))
	if m.chooser == nil || !strings.Contains(screen(m), "Google account") {
		t.Fatalf("a didn't offer the sources:\n%s", screen(m))
	}
	m, cmd := send(t, m, press("j"), press("enter"))
	m, _ = run(t, m, cmd)
	if m.gflow == nil || m.gflow.step != gClient || !strings.Contains(screen(m), "Client ID") {
		t.Fatalf("no client setup on first use:\n%s", screen(m))
	}

	m = typeText(t, m, "cid.apps.googleusercontent.com")
	m, _ = send(t, m, press("enter"), tea.PasteMsg{Content: "GOCSPX-secret"})
	if strings.Contains(screen(m), "GOCSPX-secret") {
		t.Fatal("client secret visible on screen")
	}
	m, cmd = send(t, m, press("enter"))
	for _, msg := range quickMsgs(cmd) { // start the sign-in
		m, cmd = send(t, m, msg)
	}
	if len(src.clients) != 1 || src.clients[0] == nil || *src.clients[0] != (app.GoogleClient{ID: "cid.apps.googleusercontent.com", Secret: "GOCSPX-secret"}) {
		t.Errorf("sign-in got client %v, want the typed-in one", src.clients)
	}
	if src.savedID != "" {
		t.Error("client saved before the sign-in finished")
	}
	if m.gflow.step != gWaiting {
		t.Fatalf("step = %v, want waiting for the browser", m.gflow.step)
	}
	// The whole sign-in address is shown (wrapped), in case the browser didn't open.
	sc := screen(m)
	for _, chunk := range strings.Split(ansi.Hardwrap(signInURL, min(100-4, 72)-4, false), "\n") {
		if !strings.Contains(sc, "│ "+chunk) {
			t.Errorf("sign-in address line %q not on screen:\n%s", chunk, sc)
		}
	}
	if got := activitySlot(m); !strings.HasSuffix(got, "Signing in") {
		t.Errorf("activity = %q", got)
	}

	m, _ = run(t, m, cmd) // opens the browser, waits, lists calendars
	if len(opened) != 1 || opened[0] != signInURL {
		t.Errorf("browser opened %v", opened)
	}
	if m.gflow == nil || m.gflow.step != gPick {
		t.Fatalf("no calendar picker after signing in")
	}
	sc = screen(m)
	if !strings.Contains(sc, "[x] Me") || !strings.Contains(sc, "[ ] Team") || !strings.Contains(sc, "already added") {
		t.Errorf("picker:\n%s", sc)
	}
	m, _ = send(t, m, press("j"), press("space"), press("j"), press("space")) // tick Team; Old can't be ticked
	m, cmd = send(t, m, press("enter"))
	m, _ = run(t, m, cmd)
	if len(src.added) != 2 || src.added[0].ID != "me@company.com" || src.added[1].ID != "team@group" {
		t.Errorf("added %v, want Me and Team", src.added)
	}
	if m.gflow != nil {
		t.Error("flow still open after adding")
	}
}

func TestGoogleFlowSkipsSetupWhenConfigured(t *testing.T) {
	src := &googleSource{savedID: "cid.apps.googleusercontent.com", signIn: &fakeSignIn{url: signInURL, err: errors.New("pending")}}
	m := loaded(t, 100, 30)
	m.src = src
	m.openURL = func(string) error { return nil }
	m, cmd := send(t, m, press("a"), press("2"), press("enter"))
	for range 2 { // configured? → start the sign-in
		for _, msg := range quickMsgs(cmd) {
			m, cmd = send(t, m, msg)
		}
	}
	if len(src.expects) != 1 || src.expects[0] != "" || src.clients[0] != nil || m.gflow.step != gWaiting {
		t.Errorf("expects %v step %v; want straight to sign-in", src.expects, m.gflow.step)
	}
}

func TestSignInAgainFromTheSidebar(t *testing.T) {
	work := domain.Account{ID: "g1", Provider: "google", Label: "me@company.com"}
	workCal := domain.Calendar{ID: "g1/me@company.com", AccountID: "g1", Name: "Me", ColorSlot: 3}
	src := &googleSource{savedID: "cid.apps.googleusercontent.com", signIn: &fakeSignIn{url: signInURL, acct: work}}
	m := loaded(t, 100, 30)
	m.src = src
	m.openURL = func(string) error { return nil }
	m = withCalendars(t, m, append(testCalendars, app.CalendarInfo{Account: work, Calendar: workCal}))

	m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 40), results: []app.SyncOutcome{
		{CalendarInfo: app.CalendarInfo{Account: work, Calendar: workCal}, Err: &domain.ReauthRequiredError{AccountID: "g1"}},
	}})
	if !m.needsSignIn["g1"] || !strings.Contains(screen(m), "⚠ me@company.com") || !strings.Contains(screen(m), "sign in again") {
		t.Fatalf("expired account not flagged:\n%s", screen(m))
	}

	m, _ = send(t, m, press("0"), press("G")) // the work account's calendar
	m, cmd := send(t, m, press("s"))
	if m.gflow == nil || m.gflow.expect != "me@company.com" {
		t.Fatal("s didn't start signing that account in again")
	}
	m, _ = run(t, m, cmd)
	if len(src.expects) != 1 || src.expects[0] != "me@company.com" {
		t.Errorf("sign-in expected %v", src.expects)
	}
	if m.gflow != nil || m.needsSignIn["g1"] || m.flash != flashOK {
		t.Errorf("after signing in again: flow %v, flagged %v, flash %v; want closed, cleared and synced", m.gflow != nil, m.needsSignIn["g1"], m.flash)
	}
}

func TestEscCancelsSignIn(t *testing.T) {
	signIn := &fakeSignIn{url: signInURL}
	m := loaded(t, 100, 30)
	m.src = &googleSource{savedID: "cid.apps.googleusercontent.com", signIn: signIn}
	m, _ = send(t, m, press("a"), press("2"), press("enter"))
	m, _ = send(t, m, signInStartedMsg{signIn: signIn})
	m, _ = send(t, m, press("esc"))
	if m.gflow != nil || !signIn.cancelled {
		t.Error("esc didn't cancel the sign-in")
	}
	// A result arriving after cancelling is ignored.
	m, _ = send(t, m, signInDoneMsg{acct: domain.Account{ID: "late"}})
	if m.gflow != nil {
		t.Error("a late sign-in result reopened the flow")
	}
}

func TestGooglePopupsFitTheWindow(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {60, 20}} {
		m := loaded(t, size[0], size[1])
		m.src = &googleSource{signIn: &fakeSignIn{url: signInURL}}
		chooser, _ := send(t, m, press("a"))
		client, _ := send(t, m, press("a"), press("2"), press("enter"))
		client, _ = send(t, client, googleConfiguredMsg{})
		waiting, _ := send(t, client, signInStartedMsg{signIn: &fakeSignIn{url: signInURL}})
		for name, mm := range map[string]Model{"chooser": chooser, "client": client, "waiting": waiting} {
			for i, l := range strings.Split(mm.View().Content, "\n") {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Errorf("%s %dx%d line %d width %d: %q", name, size[0], size[1], i, w, ansi.Strip(l))
				}
			}
		}
	}
}

func TestGoldenGooglePopups(t *testing.T) {
	m := loaded(t, 100, 30)
	m.src = &googleSource{}
	chooser, _ := send(t, m, press("a"))
	golden(t, "add_chooser", screen(chooser))
	waiting, _ := send(t, m, press("a"), press("2"), press("enter"))
	waiting, _ = send(t, waiting, signInStartedMsg{signIn: &fakeSignIn{url: signInURL}})
	golden(t, "google_signin", screen(waiting))
	pick, _ := send(t, waiting, signInDoneMsg{acct: domain.Account{ID: "g1", Provider: "google", Label: "me@company.com"}})
	pick, _ = send(t, pick, googleCalendarsMsg{cals: []app.RemoteCalendar{
		{ID: "me", Name: "Me", Primary: true}, {ID: "team", Name: "Team"}, {ID: "fb", Name: "Room 4B", FreeBusyOnly: true},
	}})
	golden(t, "google_pick", screen(pick))
}

// toClientForm opens add → Google account and runs until the client form.
func toClientForm(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := send(t, m, press("a"), press("2"), press("enter"))
	for _, msg := range quickMsgs(cmd) {
		m, _ = send(t, m, msg)
	}
	if m.gflow == nil || m.gflow.step != gClient {
		t.Fatalf("not on the client form:\n%s", screen(m))
	}
	return m
}

func typeClient(t *testing.T, m Model, id, secret string) (Model, tea.Cmd) {
	t.Helper()
	m = typeText(t, m, id)
	m, _ = send(t, m, press("enter"))
	m = typeText(t, m, secret)
	return send(t, m, press("enter"))
}

// The reported bug: esc after entering a client used to leave it saved, so
// the form never came back to fix a typo.
func TestEscAfterEnteringClientForgetsIt(t *testing.T) {
	src := &googleSource{signIn: &fakeSignIn{url: signInURL}}
	m := loaded(t, 100, 30)
	m.src = src
	m.openURL = func(string) error { return nil }

	m = toClientForm(t, m)
	m, cmd := typeClient(t, m, "typo.apps.googleusercontent.com", "wrong-secret")
	for _, msg := range quickMsgs(cmd) {
		m, _ = send(t, m, msg) // signing in, browser open
	}
	if m.gflow.step != gWaiting {
		t.Fatalf("step %v, want waiting", m.gflow.step)
	}
	m, _ = send(t, m, press("esc"))
	if src.savedID != "" {
		t.Fatalf("esc left the typed client saved: %q", src.savedID)
	}

	m = toClientForm(t, m) // asks again, empty
	if m.gflow.inputs[0].Value() != "" || m.gflow.inputs[1].Value() != "" {
		t.Errorf("form not empty on the next try: %q / %q", m.gflow.inputs[0].Value(), m.gflow.inputs[1].Value())
	}
}

func TestRejectedClientReturnsToTheForm(t *testing.T) {
	src := &googleSource{signIn: &fakeSignIn{url: signInURL,
		err: fmt.Errorf("%w (invalid_client)", app.ErrGoogleClientRejected)}}
	m := loaded(t, 100, 30)
	m.src = src
	m.openURL = func(string) error { return nil }

	m = toClientForm(t, m)
	m, cmd := typeClient(t, m, "mine.apps.googleusercontent.com", "typo-secret")
	m, _ = run(t, m, cmd) // sign in → Google rejects the secret
	if m.gflow == nil || m.gflow.step != gClient {
		t.Fatalf("not back on the client form after a rejection:\n%s", screen(m))
	}
	if m.gflow.inputs[0].Value() != "mine.apps.googleusercontent.com" || m.gflow.inputs[1].Value() != "" || m.gflow.focus != 1 {
		t.Errorf("form = %q / %q focus %d; want the ID kept, the secret empty and focused",
			m.gflow.inputs[0].Value(), m.gflow.inputs[1].Value(), m.gflow.focus)
	}
	if !strings.Contains(screen(m), "Google rejected the client ID or secret") {
		t.Errorf("no rejection message:\n%s", screen(m))
	}
	if src.savedID != "" {
		t.Error("a rejected client was saved")
	}
}

func TestChangeClientWhileSigningIn(t *testing.T) {
	signIn := &fakeSignIn{url: signInURL}
	src := &googleSource{savedID: "saved.apps.googleusercontent.com", signIn: signIn}
	m := loaded(t, 100, 30)
	m.src = src
	m.openURL = func(string) error { return nil }

	m, cmd := send(t, m, press("a"), press("2"), press("enter"))
	for range 2 { // saved client → sign in with it
		for _, msg := range quickMsgs(cmd) {
			m, cmd = send(t, m, msg)
		}
	}
	if m.gflow.step != gWaiting || !strings.Contains(screen(m), "c change client") {
		t.Fatalf("waiting popup lacks the change-client hint:\n%s", screen(m))
	}

	m, _ = send(t, m, press("c"))
	if !signIn.cancelled || m.gflow.step != gClient {
		t.Fatal("c didn't cancel the sign-in and reopen the client form")
	}
	if m.gflow.inputs[0].Value() != "saved.apps.googleusercontent.com" || m.gflow.inputs[1].Value() != "" {
		t.Errorf("form = %q / %q; want the saved ID and an empty secret", m.gflow.inputs[0].Value(), m.gflow.inputs[1].Value())
	}
	if strings.Contains(screen(m), "saved-secret") {
		t.Error("a secret is shown")
	}

	m = typeText(t, m, "new-secret")
	m, cmd = send(t, m, press("enter"))
	for _, msg := range quickMsgs(cmd) {
		m, _ = send(t, m, msg)
	}
	last := src.clients[len(src.clients)-1]
	if last == nil || last.ID != "saved.apps.googleusercontent.com" || last.Secret != "new-secret" {
		t.Errorf("sign-in after changing the client got %+v", last)
	}
}

func TestClientFormValidatesBeforeSigningIn(t *testing.T) {
	src := &googleSource{signIn: &fakeSignIn{url: signInURL}}
	m := loaded(t, 100, 30)
	m.src = src
	m = toClientForm(t, m)
	m, cmd := typeClient(t, m, "not-a-client-id", "s")
	if cmd != nil || len(src.expects) != 0 || !strings.Contains(screen(m), "doesn't look like an OAuth client ID") {
		t.Errorf("a malformed ID started a sign-in (or wasn't explained):\n%s", screen(m))
	}
}

func TestSyncSchedulesTheNext(t *testing.T) {
	m := loaded(t, 100, 24)
	if want := at(29, 14, 29).Add(pollInterval); !m.nextPoll.Equal(want) {
		t.Fatalf("next sync at %v, want %v", m.nextPoll, want)
	}
	stale := pollMsg{seq: m.pollSeq - 1}
	if m, _ = send(t, m, stale); m.syncing {
		t.Error("a replaced schedule started a sync")
	}
	m, cmd := send(t, m, pollMsg{seq: m.pollSeq})
	if !m.syncing || cmd == nil {
		t.Fatal("the scheduled sync didn't start")
	}
	m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 34)})
	if want := at(29, 14, 39); !m.nextPoll.Equal(want) {
		t.Errorf("next sync at %v, want %v", m.nextPoll, want)
	}
}

func TestManualSyncReplacesTheSchedule(t *testing.T) {
	m := loaded(t, 100, 24)
	scheduled := pollMsg{seq: m.pollSeq}
	m, _ = send(t, m, press("r"), syncDoneMsg{at: at(29, 14, 31)})
	if m, _ = send(t, m, scheduled); m.syncing {
		t.Error("the schedule from before r still started a sync")
	}
	if want := at(29, 14, 36); !m.nextPoll.Equal(want) {
		t.Errorf("next sync at %v, want %v", m.nextPoll, want)
	}
}

func TestScheduledSyncWaitsForOneInProgress(t *testing.T) {
	m := loaded(t, 100, 24)
	m.syncing = true // e.g. a calendar that was just added
	seq := m.pollSeq
	m, cmd := send(t, m, pollMsg{seq: seq})
	if m.pollSeq == seq || cmd == nil {
		t.Error("a scheduled sync during another wasn't rescheduled")
	}
}

func TestFailedSyncIsRetriedAndCleared(t *testing.T) {
	m := loaded(t, 140, 24)
	offline := errors.New("dial tcp: no route to host")
	m, _ = send(t, m, pollMsg{seq: m.pollSeq}, syncDoneMsg{at: at(29, 14, 34), results: []app.SyncOutcome{
		{CalendarInfo: testCalendars[0], Err: offline},
		{CalendarInfo: testCalendars[1], Err: offline},
	}})
	s := screen(m)
	if !strings.Contains(s, "no route to host (+1 more) · retry 14:39") {
		t.Errorf("status line doesn't show the failure and the retry:\n%s", s)
	}
	if !strings.Contains(s, "Dentist") {
		t.Errorf("events gone after a failed sync:\n%s", s)
	}

	// Syncing one calendar on its own leaves the other's failure alone.
	m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 35), partial: true, results: []app.SyncOutcome{{CalendarInfo: testCalendars[0]}}})
	if got := m.failureList(); len(got) != 1 || !strings.HasPrefix(got[0], testCalendars[1].Label()) {
		t.Errorf("failures after a partial sync = %q", got)
	}

	m, _ = send(t, m, pollMsg{seq: m.pollSeq}, syncDoneMsg{at: at(29, 14, 39), results: []app.SyncOutcome{
		{CalendarInfo: testCalendars[0]}, {CalendarInfo: testCalendars[1]},
	}})
	if s := screen(m); !strings.Contains(s, "Synced 14:39") || strings.Contains(s, "retry") {
		t.Errorf("failure not cleared by the next sync:\n%s", s)
	}
}

func TestRemovedCalendarsFailureIsDropped(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m,
		syncDoneMsg{at: at(29, 14, 34), results: []app.SyncOutcome{{CalendarInfo: testCalendars[1], Err: errors.New("boom")}}},
		calendarsLoadedMsg{rows: testCalendars[:1]},
	)
	if got := m.failureList(); len(got) != 0 {
		t.Errorf("failures = %q, want none for a removed calendar", got)
	}
}

func TestOverdueSyncStartsOnWaking(t *testing.T) {
	m := loaded(t, 100, 24)
	if m, _ = send(t, m, tickMsg(m.nextPoll.Add(-time.Second))); m.syncing {
		t.Fatal("a sync started before it was due")
	}
	seq := m.pollSeq
	m, _ = send(t, m, tickMsg(m.nextPoll.Add(time.Hour)))
	if !m.syncing || m.pollSeq == seq {
		t.Error("an overdue sync didn't start on the clock tick")
	}
}

type renameSource struct {
	nopSource
	cal  app.CalendarInfo
	acct domain.Account
	name string
	err  error
}

func (r *renameSource) RenameCalendar(_ context.Context, c app.CalendarInfo, name string) (app.CalendarInfo, error) {
	r.cal, r.name = c, name
	return c, r.err
}

func (r *renameSource) RenameAccount(_ context.Context, a domain.Account, name string) (domain.Account, error) {
	r.acct, r.name = a, name
	return a, r.err
}

func TestRenameCalendar(t *testing.T) {
	src := &renameSource{}
	m := loaded(t, 100, 24)
	m.src = src
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideCalendar, cal: testCalendars[0]}.key()
	m, _ = send(t, m, press("n"))
	if m.rename == nil || m.rename.account || m.rename.input.Value() != "Holidays" {
		t.Fatalf("n didn't open the rename popup with the current name: %+v", m.rename)
	}
	if s := screen(m); !strings.Contains(s, "Rename calendar") || !strings.Contains(s, "Its own name is Holidays") {
		t.Errorf("rename popup not shown:\n%s", s)
	}
	m.rename.input.SetValue("Public holidays")
	m, cmd := send(t, m, press("enter"))
	if !m.rename.saving || cmd == nil {
		t.Fatal("enter didn't save")
	}
	m, _ = send(t, m, cmd())
	if src.cal.Calendar.ID != holidays.ID || src.name != "Public holidays" {
		t.Errorf("renamed %q to %q", src.cal.Calendar.ID, src.name)
	}
	if m.rename != nil {
		t.Error("popup still open after saving")
	}

	// The reloaded list carries the nickname, shown everywhere.
	renamed := []app.CalendarInfo{testCalendars[0], testCalendars[1]}
	renamed[0].Calendar.Nickname = "Public holidays"
	renamed[1].Account.Nickname = "Home"
	m, _ = send(t, m, calendarsLoadedMsg{rows: renamed})
	if s := screen(m); !strings.Contains(s, "● Public holidays") || !strings.Contains(s, "Home · Main") {
		t.Errorf("nicknames not shown:\n%s", s)
	}
}

func TestRenameAccount(t *testing.T) {
	src := &renameSource{}
	m := loaded(t, 100, 24)
	m.src = src
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideAccount, account: personal.ID}.key()
	m, _ = send(t, m, press("n"))
	if m.rename == nil || !m.rename.account || m.rename.input.Value() != "Personal" {
		t.Fatalf("n on an account didn't open its rename popup: %+v", m.rename)
	}
	m.rename.input.SetValue("Home")
	m, cmd := send(t, m, press("enter"))
	send(t, m, cmd())
	if src.acct.ID != personal.ID || src.name != "Home" {
		t.Errorf("renamed account %q to %q", src.acct.ID, src.name)
	}
}

func TestRenameFailureKeepsPopupOpen(t *testing.T) {
	src := &renameSource{err: errors.New("disk full")}
	m := loaded(t, 100, 24)
	m.src = src
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideCalendar, cal: testCalendars[0]}.key()
	m, _ = send(t, m, press("n"))
	m, cmd := send(t, m, press("enter"))
	m, _ = send(t, m, cmd())
	if m.rename == nil || m.rename.saving || !strings.Contains(screen(m), "disk full") {
		t.Errorf("failure not shown in the popup:\n%s", screen(m))
	}
	if m, _ = send(t, m, press("esc")); m.rename != nil {
		t.Error("esc didn't close the popup")
	}
}

func TestGoldenHelp(t *testing.T) {
	m, _ := send(t, loaded(t, 120, 40), press("?"))
	golden(t, "help", screen(m))
	narrow, _ := send(t, loaded(t, 60, 20), press("?"))
	golden(t, "help_narrow", screen(narrow))
}

func TestHelpOpensScrollsAndCloses(t *testing.T) {
	m := loaded(t, 60, 20)
	m, _ = send(t, m, press("?"))
	if m.help == nil || !strings.Contains(screen(m), "Everywhere") {
		t.Fatalf("? didn't open the help:\n%s", screen(m))
	}
	m, _ = send(t, m, press("G"))
	if s := screen(m); !strings.Contains(s, "Status line") || strings.Contains(s, "Everywhere") {
		t.Errorf("G didn't scroll to the end:\n%s", s)
	}
	if m, _ = send(t, m, press("k")); m.help.scroll == 0 {
		t.Error("k at the end didn't scroll back up at once")
	}
	for _, k := range []string{"esc", "q", "?"} {
		mm, _ := send(t, m, press(k))
		if mm.help != nil {
			t.Errorf("%s didn't close the help", k)
		}
	}
}

func TestHelpFromEventDetailsReturnsThere(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("enter"), press("?"))
	if m.help == nil || m.detail == nil {
		t.Fatal("? in the event view didn't open the help over it")
	}
	if m, _ = send(t, m, press("esc")); m.help != nil || m.detail == nil {
		t.Error("closing the help didn't return to the event")
	}
}

func TestQuestionMarkIsTypedInForms(t *testing.T) {
	m := loaded(t, 100, 24)
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideCalendar, cal: testCalendars[0]}.key()
	m, _ = send(t, m, press("n"), press("?"))
	if m.help != nil || m.rename.input.Value() != "Holidays?" {
		t.Errorf("? in the rename popup: help %v, value %q", m.help != nil, m.rename.input.Value())
	}
}

func TestFitHintsDropsLeastImportantFirst(t *testing.T) {
	hints := []footHint{
		{key: "0", desc: "calendars", prio: prioHigh},
		{key: "j/k", desc: "move", prio: prioLow, newGroup: true},
		{key: "enter", desc: "details", prio: prioHigh},
		{key: "r", desc: "sync", prio: prioLow},
		{key: "?", desc: "help", prio: prioMust},
	}
	if got := ansi.Strip(fitHints(hints, 100)); got != "0 calendars │ j/k move · enter details · r sync · ? help" {
		t.Errorf("wide = %q", got)
	}
	// r sync goes before j/k move, and the group mark moves to enter.
	if got := ansi.Strip(fitHints(hints, 47)); got != "0 calendars │ j/k move · enter details · ? help" {
		t.Errorf("one dropped = %q", got)
	}
	if got := ansi.Strip(fitHints(hints, 36)); got != "0 calendars │ enter details · ? help" {
		t.Errorf("two dropped = %q", got)
	}
	if got := ansi.Strip(fitHints(hints, 4)); got != "? help" {
		t.Errorf("only the essential = %q", got)
	}
}

// agendaTitles lists the agenda's event titles, top to bottom.
func agendaTitles(m Model) []string {
	var out []string
	for _, r := range m.rows {
		if r.kind == rowItem {
			out = append(out, r.item.Event.Title)
		}
	}
	return out
}

func TestFilterNarrowsAsYouType(t *testing.T) {
	m := loaded(t, 100, 24)
	m, cmd := send(t, m, press("/"), press("d"))
	if m.filterInput == nil || m.days != filterDays || cmd == nil {
		t.Fatalf("filter didn't start or didn't load a year: input %v, days %d", m.filterInput != nil, m.days)
	}
	m = typeText(t, m, "ent")
	if got := agendaTitles(m); len(got) != 1 || got[0] != "Dentist" {
		t.Errorf("agenda filtered by %q = %q", m.filter, got)
	}
	s := screen(m)
	if !strings.Contains(s, "/dent") || !strings.Contains(s, "enter keep · esc clear") || strings.Contains(s, "Wed 30 Sep") {
		t.Errorf("screen while typing:\n%s", s)
	}

	m, _ = send(t, m, press("enter"))
	if m.filterInput != nil || m.filter != "dent" {
		t.Fatal("enter didn't keep the filter")
	}
	if s := screen(m); !strings.Contains(s, "/dent · esc clear │") {
		t.Errorf("active filter not shown in the footer:\n%s", s)
	}
	// Moving around works again, and esc clears.
	if m, _ = send(t, m, press("j")); m.filter != "dent" {
		t.Error("j changed the filter")
	}
	m, _ = send(t, m, press("esc"))
	if m.filter != "" || len(agendaTitles(m)) < 5 {
		t.Errorf("esc didn't clear the filter: %q, %q", m.filter, agendaTitles(m))
	}
}

func TestFilterMatchesEveryWordInTitleOrLocation(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("/"))
	m = typeText(t, m, "ST main")
	if got := agendaTitles(m); len(got) != 1 || got[0] != "Dentist" {
		t.Errorf("filter %q = %q, want the event at Main St", m.filter, got)
	}
	m, _ = send(t, m, press("esc"))
	if m.filter != "" || m.filterInput != nil {
		t.Error("esc while typing didn't clear and close the filter")
	}
}

func TestFilterWithoutMatches(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("/"))
	m = typeText(t, m, "zebra")
	m, _ = send(t, m, eventsLoadedMsg{from: m.from, days: m.days, events: testEvents()}) // the year it asked for
	if s := screen(m); !strings.Contains(s, "No events match “zebra” in the 365 days from Tue 29 Sep.") {
		t.Errorf("no-match text missing:\n%s", s)
	}
}

func TestFilterStepsBetweenMatchingDays(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("/"))
	m = typeText(t, m, "conference")
	m, _ = send(t, m, press("enter"))
	if day := m.currentDay(); day != domain.NewCivilDate(2026, 9, 30) {
		t.Fatalf("selected day %v, want the first match", day)
	}
	if m, _ = send(t, m, press("]")); m.currentDay() != domain.NewCivilDate(2026, 10, 1) {
		t.Errorf("] went to %v, want the next day with a match", m.currentDay())
	}
	if m, _ = send(t, m, press("]")); m.currentDay() != domain.NewCivilDate(2026, 10, 1) {
		t.Errorf("] past the last match moved to %v", m.currentDay())
	}
	if m, _ = send(t, m, press("[")); m.currentDay() != domain.NewCivilDate(2026, 9, 30) {
		t.Errorf("[ went to %v", m.currentDay())
	}
}

func TestFilterAppliesToWeekAndMonth(t *testing.T) {
	m := toWeek(t, loaded(t, 120, 30), testEvents())
	m, _ = send(t, m, press("/"))
	m = typeText(t, m, "train")
	s := screen(m)
	if !strings.Contains(s, "Night train") || strings.Contains(s, "Standup") {
		t.Errorf("week not filtered:\n%s", s)
	}
	m, _ = send(t, m, press("enter"), press("3"))
	m, _ = send(t, m, monthLoadedMsg{start: m.month.start, weeks: m.month.weeks, events: testEvents()})
	if s := screen(m); !strings.Contains(s, "Night train") || strings.Contains(s, "Standup") {
		t.Errorf("month not filtered:\n%s", s)
	}
}

func TestFilterTakesPaste(t *testing.T) {
	m := loaded(t, 100, 24)
	m, _ = send(t, m, press("/"), tea.PasteMsg{Content: "standup"})
	if got := agendaTitles(m); len(got) != 1 || got[0] != "Standup" {
		t.Errorf("pasted filter = %q, agenda %q", m.filter, got)
	}
}

// withSettings is loaded with s applied first, as cmd/clical does.
func withSettings(t *testing.T, width, height int, s Settings) Model {
	t.Helper()
	m := New(nopSource{}, loc, testNow).WithSettings(s)
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

func TestTwelveHourClock(t *testing.T) {
	m := withSettings(t, 110, 24, Settings{Clock12: true, WeekStart: time.Monday, SyncInterval: pollInterval})
	s := screen(m)
	for _, want := range []string{"9:00am–9:30am", "11:00pm–…", "── now 2:30pm", "Tue 29 Sep 2:30pm", "Synced 2:29pm"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	m, _ = send(t, m, press("enter"))
	if s := screen(m); !strings.Contains(s, "3:00pm–4:00pm") {
		t.Errorf("event view not in 12-hour time:\n%s", s)
	}
}

func TestSundayFirstWeek(t *testing.T) {
	m := withSettings(t, 120, 30, Settings{WeekStart: time.Sunday, SyncInterval: pollInterval})
	m = toWeek(t, m, testEvents())
	if m.week.start != domain.NewCivilDate(2026, 9, 27) {
		t.Errorf("week starts %v, want Sunday 27 Sep", m.week.start)
	}
	if s := screen(m); strings.Contains(s, "Week 40") {
		t.Errorf("ISO week number shown for a Sunday-first week:\n%s", s)
	}
	m, _ = send(t, m, press("3"))
	if m.month.start.In(loc).Weekday() != time.Sunday {
		t.Errorf("month grid starts on %v", m.month.start.In(loc).Weekday())
	}
}

func TestBackgroundSyncCanBeTurnedOff(t *testing.T) {
	m := withSettings(t, 100, 24, Settings{WeekStart: time.Monday})
	if !m.nextPoll.IsZero() {
		t.Errorf("a background sync was scheduled for %v with syncing off", m.nextPoll)
	}
	m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 31), results: []app.SyncOutcome{{CalendarInfo: testCalendars[0], Err: errors.New("offline")}}})
	if s := screen(m); strings.Contains(s, "retry") {
		t.Errorf("a retry was promised with syncing off:\n%s", s)
	}
}

type urlSource struct {
	nopSource
	checked string
	set     app.CalendarInfo
	err     error
}

func (u *urlSource) CheckFeed(_ context.Context, raw string) (app.Feed, error) {
	u.checked = raw
	return app.Feed{}, u.err
}

func (u *urlSource) SetCalendarURL(_ context.Context, c app.CalendarInfo, _ app.Feed) error {
	u.set = c
	return nil
}

func TestDeadFeedGetsANewURL(t *testing.T) {
	src := &urlSource{}
	m := loaded(t, 100, 24)
	m.src = src
	dead := app.SyncOutcome{CalendarInfo: testCalendars[1], Err: &domain.CalendarUnavailableError{CalendarID: mainCal.ID, Reason: "HTTP 404 Not Found"}}
	m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 31), results: []app.SyncOutcome{dead}})
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideCalendar, cal: testCalendars[1]}.key()
	s := screen(m)
	if !strings.Contains(s, "● ⚠ Main") || !strings.Contains(s, "u new URL") || !strings.Contains(s, "(u on the calendar)") {
		t.Errorf("dead feed not marked or the fix not offered:\n%s", s)
	}

	m, _ = send(t, m, press("u"))
	if m.urlForm == nil || m.urlForm.cal.Calendar.ID != mainCal.ID {
		t.Fatal("u didn't open the new-URL popup for the calendar")
	}
	if m, _ = send(t, m, press("enter")); m.urlForm.err == "" {
		t.Error("an empty address was accepted")
	}
	m, _ = send(t, m, tea.PasteMsg{Content: "https://x.test/secret-new.ics"})
	if strings.Contains(screen(m), "secret-new") {
		t.Errorf("the secret address shows on screen:\n%s", screen(m))
	}
	m, cmd := send(t, m, press("enter"))
	m, cmd = send(t, m, cmd())
	if src.checked != "https://x.test/secret-new.ics" || src.set.Calendar.ID != mainCal.ID {
		t.Errorf("checked %q, set %q", src.checked, src.set.Calendar.ID)
	}
	if m.urlForm != nil || !m.syncing || cmd == nil {
		t.Fatal("after saving: want the popup closed and the calendar syncing")
	}
	m, _ = send(t, m, syncDoneMsg{at: at(29, 14, 32), partial: true, results: []app.SyncOutcome{{CalendarInfo: testCalendars[1]}}})
	if s := screen(m); strings.Contains(s, "⚠") || strings.Contains(s, "no longer works") {
		t.Errorf("still marked after a good sync:\n%s", s)
	}
}

func TestNewURLRejectedKeepsPopupOpen(t *testing.T) {
	src := &urlSource{err: &domain.CalendarUnavailableError{CalendarID: "x", Reason: "HTTP 404 Not Found"}}
	m := loaded(t, 100, 24)
	m.src = src
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideCalendar, cal: testCalendars[1]}.key()
	m, _ = send(t, m, press("u"), tea.PasteMsg{Content: "https://x.test/wrong.ics"})
	m, cmd := send(t, m, press("enter"))
	m, _ = send(t, m, cmd())
	if m.urlForm == nil || !strings.Contains(m.urlForm.err, "HTTP 404") || src.set.Calendar.ID != "" {
		t.Errorf("rejected address: form %+v, saved %q", m.urlForm, src.set.Calendar.ID)
	}
}

func TestNewURLOnlyForICalCalendars(t *testing.T) {
	m := loaded(t, 100, 24)
	g := app.CalendarInfo{Account: domain.Account{ID: "g", Provider: "google", Label: "me@x.test"}, Calendar: domain.Calendar{ID: "g/me", AccountID: "g", Name: "Me"}}
	m = withCalendars(t, m, append(append([]app.CalendarInfo(nil), testCalendars...), g))
	m.focus = focusSidebar
	m.side.sel = sideRow{kind: sideCalendar, cal: g}.key()
	if m, _ = send(t, m, press("u")); m.urlForm != nil {
		t.Error("u opened a URL popup for a Google calendar")
	}
}
