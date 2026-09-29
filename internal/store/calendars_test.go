package store

import (
	"context"
	"testing"

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
