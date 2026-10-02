package ical

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func recurringFeed(n int, start string, rrule ...string) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:r-%d\r\nDTSTAMP:20260901T000000Z\r\n", i)
		fmt.Fprintf(&b, "DTSTART:%s\r\nDTEND:%s\r\n", start, start)
		for _, r := range rrule {
			b.WriteString(r + "\r\n")
		}
		b.WriteString("SUMMARY:Repeat\r\nEND:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// parseWithin fails the test if parsing the feed doesn't finish quickly: the
// rules below make an unguarded gocal loop forever.
func parseWithin(t *testing.T, feed string) (int, error) {
	t.Helper()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		events, err := parse(strings.NewReader(feed), "cal", from, to)
		done <- result{len(events), err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(10 * time.Second):
		t.Fatal("parse did not finish")
		return 0, nil
	}
}

func TestHostileRecurrenceRulesFinish(t *testing.T) {
	tests := []struct {
		name  string
		rrule []string
		want  int
	}{
		{"zero interval", []string{"RRULE:FREQ=DAILY;INTERVAL=0"}, 1},
		{"negative interval", []string{"RRULE:FREQ=WEEKLY;INTERVAL=-1"}, 1},
		{"escaped zero interval", []string{`RRULE:FREQ=DAILY\;INTERVAL=0`}, 1},
		{"folded zero interval", []string{"RRULE:FREQ=DAILY;INTER", " VAL=0"}, 1},
		{"count never reached by BYDAY", []string{"RRULE:FREQ=DAILY;COUNT=5;BYDAY=XX"}, 0},
		{"count never reached by BYMONTH", []string{"RRULE:FREQ=MONTHLY;COUNT=5;BYMONTH=13"}, 0},
		{"huge count", []string{"RRULE:FREQ=DAILY;COUNT=2000000000"}, 92},
		{"earlier UNTIL kept", []string{"RRULE:FREQ=DAILY;UNTIL=20261010T000000Z"}, 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := parseWithin(t, recurringFeed(1, "20261001T090000Z", tt.rrule...))
			if err != nil || n != tt.want {
				t.Errorf("parse() = %d events, %v; want %d", n, err, tt.want)
			}
		})
	}
}

func TestTooCostlyFeedIsRejected(t *testing.T) {
	// Each event steps daily from year 1: ~740k steps apiece.
	_, err := parseWithin(t, recurringFeed(100, "00010101T090000Z", "RRULE:FREQ=DAILY"))
	if !errors.Is(err, errTooComplex) {
		t.Fatalf("parse() error = %v, want %v", err, errTooComplex)
	}
}

func TestTooManyOccurrencesAreRejected(t *testing.T) {
	_, err := parseWithin(t, recurringFeed(5000, "20260901T090000Z", "RRULE:FREQ=DAILY"))
	if !errors.Is(err, errTooComplex) {
		t.Fatalf("parse() error = %v, want %v", err, errTooComplex)
	}
}

// largeFeed is a busy calendar with years of history, the way Google exports
// it: oneOffs single events spread one a day from start (most before the
// window), and series weekly meetings on three weekdays since 2020.
func largeFeed(oneOffs, series int, start time.Time) string {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n")
	for i := 0; i < oneOffs; i++ {
		s := start.Add(time.Duration(i) * 24 * time.Hour).Format("20060102T150405Z")
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:o-%d\r\nDTSTAMP:20260901T000000Z\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:One-off\r\nEND:VEVENT\r\n", i, s, s)
	}
	for i := 0; i < series; i++ {
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:s-%d\r\nDTSTAMP:20260901T000000Z\r\nDTSTART:20200106T090000Z\r\nDTEND:20200106T093000Z\r\n", i)
		b.WriteString("RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR\r\nEXDATE:20200108T090000Z\r\nSUMMARY:Weekly\r\nEND:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func TestLargeRealisticFeedIsAccepted(t *testing.T) {
	// 15k events since 1985, 40 series: ~2,100 occurrences in the window.
	feed := largeFeed(15000, 40, time.Date(1985, 1, 1, 9, 0, 0, 0, time.UTC))
	n, err := parseWithin(t, feed)
	if err != nil || n < 2000 {
		t.Fatalf("parse() = %d events, %v; want the calendar accepted", n, err)
	}
}

// RFC 5545 makes UNTIL inclusive; gocal alone gets each of these one short.
func TestUntilIsInclusive(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{"UTC, on an occurrence", []string{"DTSTART:20261001T090000Z", "DTEND:20261001T100000Z", "RRULE:FREQ=DAILY;UNTIL=20261010T090000Z"}, 10},
		{"UTC, a second before", []string{"DTSTART:20261001T090000Z", "DTEND:20261001T100000Z", "RRULE:FREQ=DAILY;UNTIL=20261010T085959Z"}, 9},
		{"date-only, timed event", []string{"DTSTART:20261001T090000Z", "DTEND:20261001T100000Z", "RRULE:FREQ=DAILY;UNTIL=20261010"}, 10},
		{"date-only, all-day event", []string{"DTSTART;VALUE=DATE:20261001", "DTEND;VALUE=DATE:20261002", "RRULE:FREQ=DAILY;UNTIL=20261010"}, 10},
		{"TZID start, UTC until", []string{"DTSTART;TZID=Europe/Stockholm:20261001T090000", "DTEND;TZID=Europe/Stockholm:20261001T100000", "RRULE:FREQ=DAILY;UNTIL=20261010T070000Z"}, 10},
		{"TZID start, UTC until a second before", []string{"DTSTART;TZID=Europe/Stockholm:20261001T090000", "DTEND;TZID=Europe/Stockholm:20261001T100000", "RRULE:FREQ=DAILY;UNTIL=20261010T065959Z"}, 9},
		{"BYDAY", []string{"DTSTART:20261005T090000Z", "DTEND:20261005T100000Z", "RRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR;UNTIL=20261016T090000Z"}, 6},
		{"RRULE before DTSTART, with an alarm", []string{"RRULE:FREQ=DAILY;UNTIL=20261010T090000Z", "BEGIN:VALARM", "TRIGGER:-PT10M", "ACTION:DISPLAY", "END:VALARM", "DTSTART:20261001T090000Z", "DTEND:20261001T100000Z"}, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u\r\nDTSTAMP:20260901T000000Z\r\n" +
				strings.Join(tt.lines, "\r\n") + "\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
			n, err := parseWithin(t, feed)
			if err != nil || n != tt.want {
				t.Errorf("parse() = %d events, %v; want %d", n, err, tt.want)
			}
		})
	}
}
