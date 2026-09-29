package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/store"
)

type fakeProvider struct {
	results map[string]domain.SyncResult
	errs    map[string]error
	tokens  map[string]string // last token seen per calendar
}

func (f *fakeProvider) SyncEvents(_ context.Context, cal domain.Calendar, token string) (domain.SyncResult, error) {
	f.tokens[cal.ID] = token
	if err := f.errs[cal.ID]; err != nil {
		return domain.SyncResult{}, err
	}
	return f.results[cal.ID], nil
}

func setup(t *testing.T) (*store.Store, *fakeProvider, *Engine) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "clical.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "fake", Label: "A"}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	for _, id := range []string{"ok", "broken"} {
		if err := s.UpsertCalendar(ctx, domain.Calendar{ID: id, AccountID: "a", Name: id}); err != nil {
			t.Fatalf("UpsertCalendar() error = %v", err)
		}
	}

	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	fp := &fakeProvider{
		results: map[string]domain.SyncResult{
			"ok": {
				Upserted:   []domain.Event{domain.NewTimedEvent("e1", "ok", "E1", start, start.Add(time.Hour), "", "")},
				NextToken:  "next",
				FullResync: true,
			},
		},
		errs:   map[string]error{"broken": errors.New("boom")},
		tokens: map[string]string{},
	}
	return s, fp, New(s, map[string]domain.CalendarProvider{"fake": fp})
}

func TestSyncAllAppliesResultsAndIsolatesFailures(t *testing.T) {
	s, fp, e := setup(t)
	ctx := context.Background()

	results, err := e.SyncAll(ctx)
	if err != nil {
		t.Fatalf("SyncAll() error = %v", err)
	}
	byCal := map[string]Result{}
	for _, r := range results {
		byCal[r.Calendar.ID] = r
	}

	if r := byCal["ok"]; r.Err != nil || !r.Changed || r.Events != 1 {
		t.Errorf("ok result = %+v, want changed with 1 event", r)
	}
	if r := byCal["broken"]; r.Err == nil {
		t.Error("broken result has no error, want boom")
	}

	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := s.EventsInRange(ctx, start, start.AddDate(0, 0, 1))
	if err != nil || len(got) != 1 {
		t.Fatalf("EventsInRange() = %v, %v; want the one synced event", got, err)
	}

	// The persisted token is passed back on the next sync.
	if _, err := e.SyncAll(ctx); err != nil {
		t.Fatalf("second SyncAll() error = %v", err)
	}
	if fp.tokens["ok"] != "next" {
		t.Errorf("second sync token = %q, want %q", fp.tokens["ok"], "next")
	}
}

func TestSyncOneNoOpIsUnchanged(t *testing.T) {
	_, fp, e := setup(t)
	fp.results["ok"] = domain.SyncResult{NextToken: "same"}

	r := e.SyncOne(context.Background(), domain.Account{ID: "a", Provider: "fake"}, domain.Calendar{ID: "ok", AccountID: "a"})
	if r.Err != nil || r.Changed {
		t.Errorf("SyncOne() = %+v, want unchanged, no error", r)
	}
}

func TestSyncOneUnknownProvider(t *testing.T) {
	_, _, e := setup(t)
	r := e.SyncOne(context.Background(), domain.Account{ID: "a", Provider: "nope"}, domain.Calendar{ID: "ok", AccountID: "a"})
	if r.Err == nil {
		t.Error("SyncOne() with unknown provider error = nil, want error")
	}
}
