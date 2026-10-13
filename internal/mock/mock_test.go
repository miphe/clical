package mock

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

var (
	loc = time.FixedZone("CEST", 2*3600)
	now = time.Date(2026, 10, 7, 14, 30, 0, 0, loc)
)

func open(t *testing.T) *Fixture {
	t.Helper()
	f, err := Open(now, loc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestMockTouchesNothingButItsOwnTemporaryDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	f := open(t)
	dir := f.dir
	if !strings.Contains(filepath.Base(dir), "clical-mock-") {
		t.Errorf("dir = %q", dir)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("the mock wrote into the user's directories: %v", entries)
	}
	f.Close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the temporary directory stayed: %v", err)
	}
}

func TestMockCalendarsCoverEveryState(t *testing.T) {
	f := open(t)
	cals, _ := f.Source.Calendars(context.Background())
	if len(cals) < 8 {
		t.Fatalf("%d calendars; want enough to pass the sixth hue", len(cals))
	}
	slots := map[int]bool{}
	for _, c := range cals {
		slots[c.Calendar.ColorSlot] = true
	}
	if len(slots) != len(cals) || !slots[7] {
		t.Errorf("slots = %v", slots)
	}
	out, _ := f.Source.Sync(context.Background())
	var unavailable, signIn int
	for _, o := range out {
		if _, ok := o.Unavailable(); ok {
			unavailable++
		}
		if o.NeedsSignIn() {
			signIn++
		}
	}
	if unavailable != 1 || signIn != 1 {
		t.Errorf("failing calendars: %d unavailable, %d needing sign-in; want one of each", unavailable, signIn)
	}
	// Everything that would reach out says no.
	if _, err := f.Source.CheckFeed(context.Background(), "https://example.com/x.ics"); err == nil {
		t.Error("CheckFeed worked")
	}
	if err := f.Source.RemoveCalendar(context.Background(), cals[0]); err == nil {
		t.Error("RemoveCalendar worked")
	}
}

func TestMockEventsAreRichAndStable(t *testing.T) {
	f := open(t)
	from, to := time.Date(2026, 10, 5, 0, 0, 0, 0, loc), time.Date(2026, 10, 12, 0, 0, 0, 0, loc)
	evs, _ := f.Source.Events(context.Background(), from, to)
	if len(evs) < 40 {
		t.Errorf("a week has %d events; want a busy one", len(evs))
	}
	ids := map[string]bool{}
	var long, allDay, links, zero bool
	for _, e := range f.Source.events {
		if ids[e.ID] {
			t.Fatalf("duplicate event ID %q", e.ID)
		}
		ids[e.ID] = true
		long = long || len(e.Title) > 60
		allDay = allDay || e.AllDay
		links = links || len(e.Links) > 0
		if s, en, ok := e.TimeRange(); ok && s.Equal(en) {
			zero = true
		}
	}
	if !long || !allDay || !links || !zero {
		t.Errorf("missing kinds: long title %v, all-day %v, links %v, zero-length %v", long, allDay, links, zero)
	}
	// Around now: one in progress, one about to start.
	soon, _ := f.Source.Events(context.Background(), now, now.Add(10*time.Minute))
	var coming bool
	for _, e := range soon {
		if e.ID == "now/quick-sync" {
			coming = true
		}
	}
	if !coming {
		t.Error("nothing starts within ten minutes of now")
	}
	// The same clock gives the same world.
	again := open(t)
	if len(again.Source.events) != len(f.Source.events) {
		t.Errorf("%d events, then %d", len(f.Source.events), len(again.Source.events))
	}
	for i := range f.Source.events {
		if f.Source.events[i].ID != again.Source.events[i].ID {
			t.Fatalf("event %d differs: %q vs %q", i, f.Source.events[i].ID, again.Source.events[i].ID)
		}
	}
}

func TestMockEventsInTheWindowOnly(t *testing.T) {
	f := open(t)
	// An all-day event of three days shows on each of them, and not on the
	// next, with an exclusive end.
	conf := domain.NewCivilDate(2026, 10, 7).AddDays(9)
	has := func(day domain.CivilDate) bool {
		evs, _ := f.Source.Events(context.Background(), day.In(loc), day.AddDays(1).In(loc))
		for _, e := range evs {
			if strings.HasPrefix(e.ID, "conf/") {
				return true
			}
		}
		return false
	}
	if has(conf.AddDays(-1)) || !has(conf) || !has(conf.AddDays(2)) || has(conf.AddDays(3)) {
		t.Error("the conference isn't on exactly its three days")
	}
}

func TestMockNotesAndArchive(t *testing.T) {
	f := open(t)
	st, err := f.App.Write()
	if err != nil {
		t.Fatal(err)
	}
	global, err := st.Get(write.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	var open, checked, notes int
	for _, it := range global.Items {
		switch {
		case !it.Todo:
			notes++
		case it.Done:
			checked++
		default:
			open++
		}
	}
	if open < 8 || checked < 2 || notes < 2 {
		t.Errorf("global list: %d open, %d checked, %d notes", open, checked, notes)
	}
	all, err := st.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 5 {
		t.Errorf("only %d lists", len(all))
	}
	var unattached bool
	for _, e := range all {
		if e.Scope.Key == "offsite-planning-cancelled" {
			unattached = true
		}
		for _, it := range e.List.Items {
			if it.Done && it.Completed.After(now) {
				t.Errorf("%q was completed in the future", it.Text)
			}
		}
	}
	if !unattached {
		t.Error("no list for a gone event")
	}

	archive, err := st.Archived()
	if err != nil {
		t.Fatal(err)
	}
	archivedNotes := 0
	for _, a := range archive {
		if a.Note {
			archivedNotes++
		}
	}
	if archivedNotes < 40 || archivedNotes > 150 {
		t.Errorf("%d archived notes; want a few dozen across the year", archivedNotes)
	}
	if len(archive)-archivedNotes < 600 {
		t.Fatalf("%d archived tasks; want a year and a half of them", len(archive))
	}
	days := write.ArchiveByDay(archive)
	// Average per calendar day, empty days included.
	var weekday, weekend, weekdays, weekends int
	var first domain.CivilDate
	for _, a := range archive {
		if !a.Note {
			first = domain.CivilDateFromTime(a.Completed)
			break
		}
	}
	for d := first; !domain.CivilDateFromTime(now).Before(d); d = d.AddDays(1) {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			weekend += len(days[d].Tasks)
			weekends++
		} else {
			weekday += len(days[d].Tasks)
			weekdays++
		}
	}
	if float64(weekday)/float64(weekdays) < 3*float64(weekend)/float64(weekends) {
		t.Errorf("weekdays average %.1f tasks, weekends %.1f; want weekdays busier", float64(weekday)/float64(weekdays), float64(weekend)/float64(weekends))
	}
	// All three intensities of the heatmap: 1-3, 4-6 and 7 or more a day.
	var low, mid, high int
	for _, day := range days {
		switch n := len(day.Tasks); {
		case n >= 7:
			high++
		case n >= 4:
			mid++
		default:
			low++
		}
	}
	if low < 20 || mid < 20 || high < 8 {
		t.Errorf("days by intensity: %d low, %d mid, %d high", low, mid, high)
	}
	// The holiday: two weeks, four months ago, with nothing done.
	away := domain.CivilDateFromTime(now.AddDate(0, 0, -126))
	empty := 0
	for i := -7; i <= 7; i++ {
		if len(days[away.AddDays(i)].Tasks) == 0 {
			empty++
		}
	}
	if empty < 14 {
		t.Errorf("only %d empty days around the holiday", empty)
	}
	// Nothing in the future, and something today.
	latest := archive[0].Completed
	for _, a := range archive {
		if a.Completed.After(latest) {
			latest = a.Completed
		}
		if a.Completed.After(now) || a.Archived.After(now) {
			t.Fatalf("%q completed %v, archived %v: after now", a.Text, a.Completed, a.Archived)
		}
	}
	// And something older than the heatmap's reach.
	if oldest := now.Sub(archive[0].Completed); oldest < 372*24*time.Hour {
		t.Errorf("the oldest task is only %v old", oldest)
	}
	var withOrigin int
	for _, a := range archive {
		if !a.Origin.IsZero() {
			withOrigin++
		}
	}
	if withOrigin < len(archive)/10 {
		t.Errorf("only %d of %d tasks have an origin", withOrigin, len(archive))
	}
}
