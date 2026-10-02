package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCivilDateBefore(t *testing.T) {
	cases := []struct {
		a, b CivilDate
		want bool
	}{
		{NewCivilDate(2026, 1, 1), NewCivilDate(2026, 1, 2), true},
		{NewCivilDate(2026, 1, 2), NewCivilDate(2026, 1, 1), false},
		{NewCivilDate(2025, 12, 31), NewCivilDate(2026, 1, 1), true},
		{NewCivilDate(2026, 1, 1), NewCivilDate(2026, 1, 1), false},
	}
	for _, c := range cases {
		if got := c.a.Before(c.b); got != c.want {
			t.Errorf("%s.Before(%s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestCivilDateAddDays(t *testing.T) {
	cases := []struct {
		start CivilDate
		n     int
		want  CivilDate
	}{
		{NewCivilDate(2026, 1, 31), 1, NewCivilDate(2026, 2, 1)},
		{NewCivilDate(2026, 2, 1), -1, NewCivilDate(2026, 1, 31)},
		{NewCivilDate(2025, 12, 31), 1, NewCivilDate(2026, 1, 1)},
		{NewCivilDate(2024, 2, 28), 1, NewCivilDate(2024, 2, 29)}, // leap year
	}
	for _, c := range cases {
		if got := c.start.AddDays(c.n); got != c.want {
			t.Errorf("%s.AddDays(%d) = %s, want %s", c.start, c.n, got, c.want)
		}
	}
}

func TestCivilDateFromTime(t *testing.T) {
	loc := time.FixedZone("test", -5*3600)
	tm := time.Date(2026, 9, 9, 23, 30, 0, 0, loc)
	got := CivilDateFromTime(tm)
	want := NewCivilDate(2026, 9, 9)
	if got != want {
		t.Errorf("CivilDateFromTime(%v) = %s, want %s", tm, got, want)
	}
}

func TestCivilDateString(t *testing.T) {
	if got, want := NewCivilDate(2026, 9, 9).String(), "2026-09-09"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestNewTimedEvent(t *testing.T) {
	start := time.Date(2026, 9, 9, 10, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	end := start.Add(time.Hour)
	ev := NewTimedEvent("e1", "cal1", "Standup", start, end, "Room A", "notes")

	gotStart, gotEnd, ok := ev.TimeRange()
	if !ok {
		t.Fatal("TimeRange ok = false, want true")
	}
	if !gotStart.Equal(start) || !gotEnd.Equal(end) {
		t.Errorf("TimeRange() = (%v, %v), want (%v, %v)", gotStart, gotEnd, start, end)
	}
	if gotStart.Location() != time.UTC {
		t.Errorf("TimeRange() start location = %v, want UTC", gotStart.Location())
	}

	if _, _, ok := ev.DateRange(); ok {
		t.Error("DateRange ok = true for a timed event, want false")
	}
}

func TestNewAllDayEvent(t *testing.T) {
	start := NewCivilDate(2026, 9, 9)
	end := NewCivilDate(2026, 9, 10)
	ev := NewAllDayEvent("e2", "cal1", "Conference", start, end, "", "")

	gotStart, gotEnd, ok := ev.DateRange()
	if !ok {
		t.Fatal("DateRange ok = false, want true")
	}
	if gotStart != start || gotEnd != end {
		t.Errorf("DateRange() = (%s, %s), want (%s, %s)", gotStart, gotEnd, start, end)
	}

	if _, _, ok := ev.TimeRange(); ok {
		t.Error("TimeRange ok = true for an all-day event, want false")
	}
}

func TestNewAccountIDIsDeterministicAndDistinct(t *testing.T) {
	a1 := NewAccountID("google", "person@example.com")
	a2 := NewAccountID("google", "person@example.com")
	if a1 != a2 {
		t.Errorf("NewAccountID is not deterministic: %q != %q", a1, a2)
	}

	b := NewAccountID("google", "other@example.com")
	if a1 == b {
		t.Errorf("NewAccountID collided for different labels: %q", a1)
	}

	c := NewAccountID("caldav", "person@example.com")
	if a1 == c {
		t.Errorf("NewAccountID collided for different providers: %q", a1)
	}
}

func TestReauthRequiredErrorUnwrap(t *testing.T) {
	base := errors.New("invalid_grant")
	err := &ReauthRequiredError{AccountID: "acct1", Err: base}

	if !errors.Is(err, base) {
		t.Error("errors.Is(err, base) = false, want true")
	}

	var target *ReauthRequiredError
	if !errors.As(err, &target) {
		t.Fatal("errors.As failed to match *ReauthRequiredError")
	}
	if target.AccountID != "acct1" {
		t.Errorf("AccountID = %q, want %q", target.AccountID, "acct1")
	}
}

func TestParseCivilDateRoundTrip(t *testing.T) {
	d := NewCivilDate(2026, 2, 3)
	got, err := ParseCivilDate(d.String())
	if err != nil {
		t.Fatalf("ParseCivilDate() error = %v", err)
	}
	if got != d {
		t.Errorf("ParseCivilDate(%q) = %s, want %s", d.String(), got, d)
	}
	if _, err := ParseCivilDate("2026-13-01"); err == nil {
		t.Error("ParseCivilDate(invalid) error = nil, want error")
	}
}

func TestCivilDateIn(t *testing.T) {
	loc := time.FixedZone("test", 3*3600)
	got := NewCivilDate(2026, 9, 29).In(loc)
	if want := time.Date(2026, 9, 29, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("In() = %v, want %v", got, want)
	}
}

func TestCivilDateWeekday(t *testing.T) {
	if got := NewCivilDate(2026, 10, 2).Weekday(); got != time.Friday {
		t.Errorf("2026-10-02 Weekday() = %v, want Friday", got)
	}
}
