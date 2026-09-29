package store

import (
	"context"
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
