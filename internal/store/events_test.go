package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func seedCalendar(t *testing.T, s *Store) domain.Calendar {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "ical", Label: "A"}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	cal := domain.Calendar{ID: "c", AccountID: "a", Name: "C"}
	if err := s.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("UpsertCalendar() error = %v", err)
	}
	return cal
}

func ids(events []domain.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.ID)
	}
	sort.Strings(out)
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var (
	day   = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	timed = func(id string, start time.Time, d time.Duration) domain.Event {
		return domain.NewTimedEvent(id, "c", id, start, start.Add(d), "loc", "notes")
	}
	allDay = func(id string, y, m, d, days int) domain.Event {
		s := domain.NewCivilDate(y, m, d)
		return domain.NewAllDayEvent(id, "c", id, s, s.AddDays(days), "", "")
	}
)

func TestApplySyncFullResyncReplacesAndPersistsToken(t *testing.T) {
	s := openTestStore(t)
	cal := seedCalendar(t, s)
	ctx := context.Background()
	window := func() []string {
		got, err := s.EventsInRange(ctx, day.AddDate(0, 0, -1), day.AddDate(0, 0, 2))
		if err != nil {
			t.Fatalf("EventsInRange() error = %v", err)
		}
		return ids(got)
	}

	if err := s.ApplySync(ctx, cal, domain.SyncResult{
		Upserted:   []domain.Event{timed("t1", day.Add(9*time.Hour), time.Hour), allDay("d1", 2026, 10, 1, 1)},
		NextToken:  "tok1",
		FullResync: true,
	}); err != nil {
		t.Fatalf("ApplySync() error = %v", err)
	}
	if got := window(); !equalIDs(got, []string{"d1", "t1"}) {
		t.Errorf("after first sync ids = %v, want [d1 t1]", got)
	}

	if err := s.ApplySync(ctx, cal, domain.SyncResult{
		Upserted:   []domain.Event{timed("t2", day.Add(10*time.Hour), time.Hour)},
		NextToken:  "tok2",
		FullResync: true,
	}); err != nil {
		t.Fatalf("second ApplySync() error = %v", err)
	}
	if got := window(); !equalIDs(got, []string{"t2"}) {
		t.Errorf("after full resync ids = %v, want [t2]", got)
	}

	tok, err := s.SyncToken(ctx, cal)
	if err != nil || tok != "tok2" {
		t.Errorf("SyncToken() = %q, %v; want tok2", tok, err)
	}
}

func TestApplySyncDeltaDeletes(t *testing.T) {
	s := openTestStore(t)
	cal := seedCalendar(t, s)
	ctx := context.Background()

	if err := s.ApplySync(ctx, cal, domain.SyncResult{
		Upserted:   []domain.Event{timed("t1", day, time.Hour), timed("t2", day.Add(time.Hour), time.Hour)},
		FullResync: true,
	}); err != nil {
		t.Fatalf("ApplySync() error = %v", err)
	}
	if err := s.ApplySync(ctx, cal, domain.SyncResult{DeletedIDs: []string{"t1"}}); err != nil {
		t.Fatalf("delta ApplySync() error = %v", err)
	}
	got, err := s.EventsInRange(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("EventsInRange() error = %v", err)
	}
	if !equalIDs(ids(got), []string{"t2"}) {
		t.Errorf("ids = %v, want [t2]", ids(got))
	}
}

func TestEventsInRangeOverlap(t *testing.T) {
	s := openTestStore(t)
	cal := seedCalendar(t, s)
	ctx := context.Background()

	if err := s.ApplySync(ctx, cal, domain.SyncResult{
		Upserted: []domain.Event{
			timed("before", day.Add(-2*time.Hour), time.Hour),      // ends before window
			timed("straddle", day.Add(-30*time.Minute), time.Hour), // crosses window start
			timed("inside", day.Add(12*time.Hour), time.Hour),      // inside
			timed("zero", day.Add(13*time.Hour), 0),                // zero-length inside
			timed("endsAtStart", day.Add(-time.Hour), time.Hour),   // ends exactly at window start
			timed("after", day.AddDate(0, 0, 1), time.Hour),        // starts at window end
			allDay("prevDay", 2026, 9, 30, 1),                      // day before window
			allDay("multi", 2026, 9, 30, 3),                        // spans into window
			allDay("nextDay", 2026, 10, 2, 1),                      // day after window
		},
		FullResync: true,
	}); err != nil {
		t.Fatalf("ApplySync() error = %v", err)
	}

	got, err := s.EventsInRange(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("EventsInRange() error = %v", err)
	}
	want := []string{"inside", "multi", "straddle", "zero"}
	if !equalIDs(ids(got), want) {
		t.Errorf("ids = %v, want %v", ids(got), want)
	}
}

func TestEventsInRangeRoundTripsFields(t *testing.T) {
	s := openTestStore(t)
	cal := seedCalendar(t, s)
	ctx := context.Background()

	start := time.Date(2026, 10, 1, 9, 30, 0, 0, time.FixedZone("EEST", 3*3600))
	in := domain.NewTimedEvent("t", "c", "Dentist", start, start.Add(45*time.Minute), "Main St", "Bring card")
	if err := s.ApplySync(ctx, cal, domain.SyncResult{Upserted: []domain.Event{in}, FullResync: true}); err != nil {
		t.Fatalf("ApplySync() error = %v", err)
	}
	got, err := s.EventsInRange(ctx, day, day.AddDate(0, 0, 1))
	if err != nil || len(got) != 1 {
		t.Fatalf("EventsInRange() = %v, %v; want one event", got, err)
	}
	e := got[0]
	gs, ge, ok := e.TimeRange()
	if !ok || !gs.Equal(start) || !ge.Equal(start.Add(45*time.Minute)) {
		t.Errorf("TimeRange() = %v–%v ok=%v, want %v–%v", gs, ge, ok, start, start.Add(45*time.Minute))
	}
	if e.Title != "Dentist" || e.Location != "Main St" || e.Notes != "Bring card" || e.CalendarID != "c" {
		t.Errorf("fields = %+v", e)
	}
}

func TestLinksRoundTrip(t *testing.T) {
	s := openTestStore(t)
	cal := seedCalendar(t, s)
	ctx := context.Background()
	links := []domain.Link{
		{URL: "https://meet.google.com/abc", Label: "Join Google Meet", Kind: domain.LinkVideoCall},
		{URL: "https://docs.google.com/document/d/1", Label: "Agenda", Kind: domain.LinkDocument},
	}
	e := timed("with-links", day.Add(9*time.Hour), time.Hour).WithLinks(links)
	if err := s.ApplySync(ctx, cal, domain.SyncResult{Upserted: []domain.Event{e, timed("none", day.Add(11*time.Hour), time.Hour)}, FullResync: true}); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsInRange(ctx, day, day.AddDate(0, 0, 1))
	if err != nil || len(got) != 2 {
		t.Fatalf("EventsInRange() = %d events, %v", len(got), err)
	}
	for _, g := range got {
		switch g.ID {
		case "with-links":
			if len(g.Links) != 2 || g.Links[0] != links[0] || g.Links[1] != links[1] {
				t.Errorf("links = %+v, want %+v", g.Links, links)
			}
		case "none":
			if len(g.Links) != 0 {
				t.Errorf("event without links got %+v", g.Links)
			}
		}
	}
}

func TestMigrationAddsLinksColumnToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// A database from before links existed.
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE events (id TEXT NOT NULL, calendar_id TEXT NOT NULL, account_id TEXT NOT NULL,
		title TEXT NOT NULL, all_day INTEGER NOT NULL, start_utc INTEGER, end_utc INTEGER, start_date TEXT, end_date TEXT,
		location TEXT NOT NULL DEFAULT '', notes TEXT NOT NULL DEFAULT '', PRIMARY KEY (account_id, calendar_id, id))`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on an old database error = %v", err)
	}
	defer s.Close()
	if ok, err := s.hasColumn(context.Background(), "events", "links"); err != nil || !ok {
		t.Errorf("links column after migration: %v, %v", ok, err)
	}
}

func TestMigrationAddsNicknameColumnsToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// A database from before nicknames existed.
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE accounts (id TEXT PRIMARY KEY, provider TEXT NOT NULL, label TEXT NOT NULL);
		CREATE TABLE calendars (id TEXT NOT NULL, account_id TEXT NOT NULL, name TEXT NOT NULL, color TEXT NOT NULL,
			sync_token TEXT, last_synced_at INTEGER, PRIMARY KEY (account_id, id));
		INSERT INTO accounts VALUES ('a', 'ical', 'Personal');
		INSERT INTO calendars (id, account_id, name, color) VALUES ('c', 'a', 'Main', '1');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on an old database error = %v", err)
	}
	defer s.Close()
	cals, err := s.ListCalendars(context.Background())
	if err != nil || len(cals) != 1 || cals[0].DisplayName() != "Main" {
		t.Errorf("ListCalendars() after migration = %+v, %v", cals, err)
	}
}
