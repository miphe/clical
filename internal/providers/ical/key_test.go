package ical

import (
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func parseLines(t *testing.T, lines ...string) []domain.Event {
	t.Helper()
	feed := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" + strings.Join(lines, "\r\n") + "\r\nEND:VCALENDAR\r\n"
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	events, err := parse(strings.NewReader(feed), "cal", from, from.AddDate(0, 6, 0))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func oneOff(uid, start, end string) []string {
	return []string{"BEGIN:VEVENT", "UID:" + uid, "DTSTAMP:20260901T000000Z", "DTSTART:" + start, "DTEND:" + end, "SUMMARY:Meeting", "END:VEVENT"}
}

func TestKeySurvivesRescheduling(t *testing.T) {
	before := parseLines(t, oneOff("m1", "20261001T100000Z", "20261001T110000Z")...)
	after := parseLines(t, oneOff("m1", "20261003T153000Z", "20261003T163000Z")...)
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("parsed %d and %d events", len(before), len(after))
	}
	if before[0].ID == after[0].ID {
		t.Fatal("setup: the ID was expected to change with the start")
	}
	if before[0].Key != "m1" || after[0].Key != "m1" || before[0].Identity() != after[0].Identity() {
		t.Errorf("keys %q and %q; want m1 for both", before[0].Key, after[0].Key)
	}
}

func TestKeysOfRecurringOccurrences(t *testing.T) {
	events := parseLines(t,
		"BEGIN:VEVENT", "UID:weekly", "DTSTAMP:20260901T000000Z", "DTSTART:20261006T090000Z", "DTEND:20261006T091500Z",
		"RRULE:FREQ=WEEKLY;COUNT=3", "SUMMARY:Standup", "END:VEVENT",
		"BEGIN:VEVENT", "UID:weekly", "DTSTAMP:20260901T000000Z", "RECURRENCE-ID:20261013T090000Z",
		"DTSTART:20261014T130000Z", "DTEND:20261014T131500Z", "SUMMARY:Standup (moved)", "END:VEVENT",
	)
	keys := map[string]string{}
	for _, e := range events {
		keys[e.Title+"@"+e.Key] = e.Title
	}
	for _, want := range []string{
		"Standup@weekly/2026-10-06T09:00:00Z",
		"Standup@weekly/2026-10-20T09:00:00Z",
		// The moved occurrence keeps the key of the one it replaced.
		"Standup (moved)@weekly/2026-10-13T09:00:00Z",
	} {
		if _, ok := keys[want]; !ok {
			t.Errorf("no occurrence %q in %v", want, keys)
		}
	}
	if len(events) != 3 {
		t.Errorf("%d occurrences; want 3", len(events))
	}
}

func TestKeyOfAllDayEvents(t *testing.T) {
	one := parseLines(t, "BEGIN:VEVENT", "UID:day", "DTSTAMP:20260901T000000Z", "DTSTART;VALUE=DATE:20261005", "DTEND;VALUE=DATE:20261006", "SUMMARY:Day", "END:VEVENT")
	if len(one) != 1 || one[0].Key != "day" {
		t.Errorf("one-off all-day key = %+v", one)
	}
	rec := parseLines(t, "BEGIN:VEVENT", "UID:daily", "DTSTAMP:20260901T000000Z", "DTSTART;VALUE=DATE:20261005", "DTEND;VALUE=DATE:20261006", "RRULE:FREQ=DAILY;COUNT=2", "SUMMARY:Rec", "END:VEVENT")
	if len(rec) != 2 || rec[0].Key == rec[1].Key || !strings.HasPrefix(rec[0].Key, "daily/2026-10-0") {
		t.Errorf("recurring all-day keys = %q, %q", rec[0].Key, rec[1].Key)
	}
}
