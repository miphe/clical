package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func TestUpsertAndListCalendars(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, a := range []domain.Account{
		{ID: "work", Provider: "ical", Label: "Work"},
		{ID: "personal", Provider: "ical", Label: "Personal"},
	} {
		if err := s.UpsertAccount(ctx, a); err != nil {
			t.Fatalf("UpsertAccount(%s) error = %v", a.ID, err)
		}
	}
	for _, c := range []domain.Calendar{
		{ID: "c1", AccountID: "work", Name: "Team"},
		{ID: "c2", AccountID: "personal", Name: "Family"},
		{ID: "c3", AccountID: "personal", Name: "Birthdays"},
	} {
		if err := s.UpsertCalendar(ctx, c); err != nil {
			t.Fatalf("UpsertCalendar(%s) error = %v", c.ID, err)
		}
	}

	got, err := s.ListCalendars(ctx)
	if err != nil {
		t.Fatalf("ListCalendars() error = %v", err)
	}
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	// Personal (Birthdays, Family) sorts before Work (Team).
	want := []string{"c3", "c2", "c1"}
	if len(ids) != len(want) {
		t.Fatalf("ListCalendars() ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ListCalendars() ids = %v, want %v", ids, want)
		}
	}
}

func TestUpsertCalendarRenamesInPlace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "ical", Label: "A"}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	cal := domain.Calendar{ID: "c", AccountID: "a", Name: "Old"}
	if err := s.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("UpsertCalendar() error = %v", err)
	}
	cal.Name = "New"
	if err := s.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("UpsertCalendar() rename error = %v", err)
	}

	got, err := s.ListCalendars(ctx)
	if err != nil {
		t.Fatalf("ListCalendars() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "New" {
		t.Errorf("ListCalendars() = %+v, want one calendar named New", got)
	}
}

func TestUpsertCalendarRequiresAccount(t *testing.T) {
	s := openTestStore(t)
	err := s.UpsertCalendar(context.Background(), domain.Calendar{ID: "c", AccountID: "missing", Name: "X"})
	if err == nil {
		t.Error("UpsertCalendar() for unknown account error = nil, want foreign key violation")
	}
}

func TestDeleteCalendarRemovesEventsAndEmptyAccount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "ical", Label: "A"}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	c1 := domain.Calendar{ID: "c1", AccountID: "a", Name: "One"}
	c2 := domain.Calendar{ID: "c2", AccountID: "a", Name: "Two"}
	for _, c := range []domain.Calendar{c1, c2} {
		if err := s.UpsertCalendar(ctx, c); err != nil {
			t.Fatalf("UpsertCalendar() error = %v", err)
		}
	}
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if err := s.ApplySync(ctx, c1, domain.SyncResult{
		Upserted:   []domain.Event{domain.NewTimedEvent("e", "c1", "E", start, start.Add(time.Hour), "", "")},
		FullResync: true,
	}); err != nil {
		t.Fatalf("ApplySync() error = %v", err)
	}

	if err := s.DeleteCalendar(ctx, c1); err != nil {
		t.Fatalf("DeleteCalendar(c1) error = %v", err)
	}
	events, err := s.EventsInRange(ctx, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil || len(events) != 0 {
		t.Errorf("events after delete = %v, %v; want none", events, err)
	}
	if accts, _ := s.ListAccounts(ctx); len(accts) != 1 {
		t.Errorf("accounts after deleting one of two calendars = %d, want 1", len(accts))
	}

	if err := s.DeleteCalendar(ctx, c2); err != nil {
		t.Fatalf("DeleteCalendar(c2) error = %v", err)
	}
	if accts, _ := s.ListAccounts(ctx); len(accts) != 0 {
		t.Errorf("accounts after deleting last calendar = %d, want 0", len(accts))
	}
}

func TestResetSyncToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "ical", Label: "A"}); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	cal := domain.Calendar{ID: "c", AccountID: "a", Name: "C"}
	if err := s.UpsertCalendar(ctx, cal); err != nil {
		t.Fatalf("UpsertCalendar() error = %v", err)
	}
	if err := s.ApplySync(ctx, cal, domain.SyncResult{NextToken: "tok"}); err != nil {
		t.Fatalf("ApplySync() error = %v", err)
	}
	if err := s.ResetSyncToken(ctx, cal); err != nil {
		t.Fatalf("ResetSyncToken() error = %v", err)
	}
	if tok, err := s.SyncToken(ctx, cal); err != nil || tok != "" {
		t.Errorf("SyncToken() after reset = %q, %v; want empty", tok, err)
	}
}

func TestColorSlotRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "ical", Label: "A"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []domain.Calendar{
		{ID: "unset", AccountID: "a", Name: "Unset"},
		{ID: "seven", AccountID: "a", Name: "Seven", ColorSlot: 7},
	} {
		if err := s.UpsertCalendar(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	cals, err := s.ListCalendars(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, c := range cals {
		got[c.ID] = c.ColorSlot
	}
	if got["unset"] != 0 || got["seven"] != 7 {
		t.Errorf("slots = %v, want unset:0 seven:7", got)
	}
}

func TestNicknames(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, a := range []domain.Account{
		{ID: "a", Provider: "ical", Label: "Zeta"},
		{ID: "b", Provider: "ical", Label: "Beta"},
	} {
		if err := s.UpsertAccount(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	cal := domain.Calendar{ID: "c1", AccountID: "a", Name: "Team"}
	for _, c := range []domain.Calendar{cal, {ID: "c2", AccountID: "b", Name: "Other"}} {
		if err := s.UpsertCalendar(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetAccountNickname(ctx, "a", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCalendarNickname(ctx, cal, "Squad\x1b[2J"); err != nil {
		t.Fatal(err)
	}
	// Updates from the provider keep the nicknames.
	if err := s.UpsertAccount(ctx, domain.Account{ID: "a", Provider: "ical", Label: "Zeta"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertCalendar(ctx, domain.Calendar{ID: "c1", AccountID: "a", Name: "Team 2"}); err != nil {
		t.Fatal(err)
	}

	cals, err := s.ListCalendars(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by the names shown: Alpha (was Zeta) before Beta.
	if len(cals) != 2 || cals[0].ID != "c1" || cals[0].Name != "Team 2" || cals[0].Nickname != "Squad[2J" {
		t.Errorf("ListCalendars() = %+v", cals)
	}
	if acct, err := s.GetAccount(ctx, "a"); err != nil || acct.Nickname != "Alpha" || acct.Label != "Zeta" {
		t.Errorf("GetAccount() = %+v, %v", acct, err)
	}
	if err := s.SetCalendarNickname(ctx, domain.Calendar{ID: "nope", AccountID: "a"}, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetCalendarNickname on a missing calendar error = %v", err)
	}
	if err := s.SetAccountNickname(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetAccountNickname on a missing account error = %v", err)
	}
}
