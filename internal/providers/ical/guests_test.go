package ical

import (
	"strings"
	"testing"
	"time"

	"github.com/miphe/clical/internal/domain"
)

func TestGuestsCarryResponseRoleAndOrganizer(t *testing.T) {
	e := parseOne(t,
		`ORGANIZER;CN=Jane Doe:mailto:jane@example.com`,
		`ATTENDEE;CUTYPE=INDIVIDUAL;ROLE=REQ-PARTICIPANT;PARTSTAT=ACCEPTED;CN=Jane Doe:mailto:jane@example.com`,
		`ATTENDEE;ROLE=OPT-PARTICIPANT;PARTSTAT=TENTATIVE;CN="Smith, Bob":mailto:bob@example.com`,
		`ATTENDEE;PARTSTAT=DECLINED:mailto:carol@example.com`,
		`ATTENDEE;PARTSTAT=NEEDS-ACTION;CN=Dave:mailto:dave@example.com`,
		`ATTENDEE;CUTYPE=ROOM;PARTSTAT=ACCEPTED;CN=Room 4B:mailto:room4b@example.com`,
		`BEGIN:VALARM`, `ACTION:EMAIL`, `ATTENDEE:mailto:alarm@example.com`, `END:VALARM`,
	)
	want := []domain.Guest{
		{Name: "Jane Doe", Email: "jane@example.com", Response: domain.ResponseYes, Organizer: true},
		{Email: "carol@example.com", Response: domain.ResponseNo},
		{Name: "Dave", Email: "dave@example.com"},
		{Name: "Smith, Bob", Email: "bob@example.com", Response: domain.ResponseMaybe, Optional: true},
	}
	if len(e.Guests) != len(want) {
		t.Fatalf("guests = %+v, want %+v", e.Guests, want)
	}
	for i, w := range want {
		if e.Guests[i] != w {
			t.Errorf("guest %d = %+v, want %+v", i, e.Guests[i], w)
		}
	}
}

func TestGuestsOfAnOverriddenOccurrenceAreItsOwn(t *testing.T) {
	feed := strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//test//EN",
		"BEGIN:VEVENT", "UID:s", "DTSTAMP:20260901T000000Z", "DTSTART:20261006T080000Z", "DTEND:20261006T083000Z",
		"SUMMARY:Series", "RRULE:FREQ=WEEKLY;COUNT=2", "ATTENDEE;CN=Ann:mailto:ann@example.com", "END:VEVENT",
		"BEGIN:VEVENT", "UID:s", "DTSTAMP:20260901T000000Z", "RECURRENCE-ID:20261013T080000Z", "DTSTART:20261013T090000Z", "DTEND:20261013T093000Z",
		"SUMMARY:Series", "ATTENDEE;CN=Ben:mailto:ben@example.com", "END:VEVENT",
		"END:VCALENDAR", "",
	}, "\r\n")
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	events, err := parse(strings.NewReader(feed), "cal", from, from.AddDate(0, 3, 0))
	if err != nil || len(events) != 2 {
		t.Fatalf("parse() = %d events, %v", len(events), err)
	}
	start := func(e domain.Event) time.Time { s, _, _ := e.TimeRange(); return s }
	if start(events[0]).After(start(events[1])) {
		events[0], events[1] = events[1], events[0]
	}
	if names := []string{events[0].Guests[0].Name, events[1].Guests[0].Name}; names[0] != "Ann" || names[1] != "Ben" {
		t.Errorf("guests by occurrence = %v, want Ann then Ben", names)
	}
}
