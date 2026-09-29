package ical

import (
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

var fixtureLines = []string{
	"BEGIN:VCALENDAR",
	"VERSION:2.0",
	"PRODID:-//test//EN",

	"BEGIN:VEVENT",
	"UID:timed-1",
	"DTSTAMP:20260901T000000Z",
	"DTSTART;TZID=Europe/Helsinki:20261001T100000",
	"DTEND;TZID=Europe/Helsinki:20261001T110000",
	"SUMMARY:Dentist",
	"LOCATION:Main St 1",
	"DESCRIPTION:Bring card",
	"END:VEVENT",

	"BEGIN:VEVENT",
	"UID:allday-1",
	"DTSTAMP:20260901T000000Z",
	"DTSTART;VALUE=DATE:20261002",
	"DTEND;VALUE=DATE:20261003",
	"SUMMARY:Holiday",
	"END:VEVENT",

	"BEGIN:VEVENT",
	"UID:allday-2",
	"DTSTAMP:20260901T000000Z",
	"DTSTART;VALUE=DATE:20261005",
	"DTEND;VALUE=DATE:20261007",
	"SUMMARY:Conference",
	"END:VEVENT",

	"BEGIN:VEVENT",
	"UID:weekly-1",
	"DTSTAMP:20260901T000000Z",
	"DTSTART:20261006T090000Z",
	"DTEND:20261006T091500Z",
	"RRULE:FREQ=WEEKLY;COUNT=3",
	"EXDATE:20261013T090000Z",
	"SUMMARY:Standup",
	"END:VEVENT",

	"BEGIN:VEVENT",
	"UID:weekly-1",
	"DTSTAMP:20260901T000000Z",
	"RECURRENCE-ID:20261020T090000Z",
	"DTSTART:20261021T100000Z",
	"DTEND:20261021T101500Z",
	"SUMMARY:Standup (moved)",
	"END:VEVENT",

	"BEGIN:VEVENT",
	"UID:cancelled-1",
	"DTSTAMP:20260901T000000Z",
	"DTSTART:20261008T120000Z",
	"DTEND:20261008T130000Z",
	"STATUS:CANCELLED",
	"SUMMARY:Called off",
	"END:VEVENT",

	"END:VCALENDAR",
}

func parseFixture(t *testing.T) map[string]domain.Event {
	t.Helper()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	events, err := parse(strings.NewReader(strings.Join(fixtureLines, "\r\n")+"\r\n"), "cal1", from, to)
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	byKey := map[string]domain.Event{}
	for _, e := range events {
		byKey[e.Title+"@"+e.ID] = e
	}
	return byKey
}

func find(t *testing.T, events map[string]domain.Event, title string) []domain.Event {
	t.Helper()
	var out []domain.Event
	for k, e := range events {
		if strings.HasPrefix(k, title+"@") {
			out = append(out, e)
		}
	}
	return out
}

func TestParseTimedEventWithTZID(t *testing.T) {
	got := find(t, parseFixture(t), "Dentist")
	if len(got) != 1 {
		t.Fatalf("got %d Dentist events, want 1", len(got))
	}
	start, end, ok := got[0].TimeRange()
	if !ok {
		t.Fatal("Dentist is not a timed event")
	}
	wantStart := time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC) // 10:00 EEST = 07:00 UTC
	if !start.Equal(wantStart) || !end.Equal(wantStart.Add(time.Hour)) {
		t.Errorf("TimeRange() = %v–%v, want %v–%v", start, end, wantStart, wantStart.Add(time.Hour))
	}
	if got[0].Location != "Main St 1" || got[0].Notes != "Bring card" || got[0].CalendarID != "cal1" {
		t.Errorf("event fields = %+v", got[0])
	}
}

func TestParseAllDayEvents(t *testing.T) {
	events := parseFixture(t)
	cases := []struct {
		title      string
		start, end domain.CivilDate
	}{
		{"Holiday", domain.NewCivilDate(2026, 10, 2), domain.NewCivilDate(2026, 10, 3)},
		{"Conference", domain.NewCivilDate(2026, 10, 5), domain.NewCivilDate(2026, 10, 7)},
	}
	for _, c := range cases {
		got := find(t, events, c.title)
		if len(got) != 1 {
			t.Fatalf("got %d %s events, want 1", len(got), c.title)
		}
		start, end, ok := got[0].DateRange()
		if !ok {
			t.Fatalf("%s is not an all-day event", c.title)
		}
		if start != c.start || end != c.end {
			t.Errorf("%s DateRange() = %s–%s, want %s–%s", c.title, start, end, c.start, c.end)
		}
	}
}

func TestParseRecurrenceWithExdateAndOverride(t *testing.T) {
	events := parseFixture(t)

	regular := find(t, events, "Standup")
	moved := find(t, events, "Standup (moved)")
	if len(regular) != 1 {
		t.Fatalf("got %d regular Standup occurrences, want 1 (Oct 13 excluded, Oct 20 overridden)", len(regular))
	}
	if len(moved) != 1 {
		t.Fatalf("got %d moved Standup occurrences, want 1", len(moved))
	}

	if start, _, _ := regular[0].TimeRange(); !start.Equal(time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("regular Standup start = %v, want 2026-10-06 09:00 UTC", start)
	}
	if start, _, _ := moved[0].TimeRange(); !start.Equal(time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("moved Standup start = %v, want 2026-10-21 10:00 UTC", start)
	}
	if regular[0].ID == moved[0].ID {
		t.Errorf("occurrences share ID %q, want distinct", regular[0].ID)
	}
}

func TestParseSkipsCancelled(t *testing.T) {
	if got := find(t, parseFixture(t), "Called off"); len(got) != 0 {
		t.Errorf("got %d cancelled events, want 0", len(got))
	}
}
