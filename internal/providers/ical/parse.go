package ical

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apognu/gocal"
	"github.com/apognu/gocal/parser"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/eventtext"
)

// parse reads an iCalendar feed and returns every non-cancelled event
// overlapping [from, to), with recurring events expanded into one Event per
// occurrence.
func parse(r io.Reader, calendarID string, from, to time.Time) ([]domain.Event, error) {
	feed, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("ical: read feed: %w", err)
	}
	if feed, err = guardRecurrence(feed, from, to); err != nil {
		return nil, fmt.Errorf("ical: parse feed: %w", err)
	}
	p := gocal.NewParser(bytes.NewReader(feed))
	p.Start, p.End = &from, &to
	// All-day dates are read as UTC midnight so converting back to a civil
	// date never shifts the day, whatever the local timezone is.
	p.AllDayEventsTZ = time.UTC
	if err := p.Parse(); err != nil {
		return nil, fmt.Errorf("ical: parse feed: %w", err)
	}

	out := make([]domain.Event, 0, len(p.Events))
	for _, e := range p.Events {
		if e.Start == nil || strings.EqualFold(e.Status, "CANCELLED") {
			continue
		}
		out = append(out, toDomainEvent(calendarID, e))
	}
	return out, nil
}

// newlines decodes iCalendar's escaped line breaks ("\n", "\N"). gocal
// decodes the other TEXT escapes (\\ \; \,) but not these, so without this a
// multi-line description shows literal backslash-n sequences.
var newlines = strings.NewReplacer(`\n`, "\n", `\N`, "\n")

func toDomainEvent(calendarID string, e gocal.Event) domain.Event {
	e.Summary = newlines.Replace(e.Summary)
	e.Location = newlines.Replace(e.Location)
	e.Description = newlines.Replace(e.Description)
	if alt, ok := e.CustomAttributes["X-ALT-DESC"]; ok {
		attrs := make(map[string]string, len(e.CustomAttributes))
		for k, v := range e.CustomAttributes {
			attrs[k] = v
		}
		attrs["X-ALT-DESC"] = newlines.Replace(alt)
		e.CustomAttributes = attrs
	}
	notes, links := eventtext.Describe(eventtext.Content{
		Description: e.Description,
		AltHTML:     e.CustomAttributes["X-ALT-DESC"],
		Location:    e.Location,
		URL:         e.URL,
		Conference:  []string{e.CustomAttributes["X-GOOGLE-CONFERENCE"], e.CustomAttributes["X-MICROSOFT-SKYPETEAMSMEETINGURL"]},
	})
	e.Description = notes
	return toDomainEventWithNotes(calendarID, e).WithLinks(links).WithKey(eventKey(e))
}

// eventKey is an event's stable identity: its UID for a one-off event, and
// UID plus the occurrence's original start for a recurring one, so a moved
// occurrence (RECURRENCE-ID) keeps the key of the one it replaced. Unlike
// the event ID it doesn't change when the event is rescheduled.
func eventKey(e gocal.Event) string {
	switch {
	case e.RecurrenceID != "":
		if orig, err := parser.ParseTime(e.RecurrenceID, e.RawStart.Params, parser.TimeStart, false, time.UTC); err == nil {
			return eventID(e.Uid, occurrenceStamp(e, *orig))
		}
		return e.Uid
	case e.IsRecurring:
		return eventID(e.Uid, occurrenceStamp(e, *e.Start))
	}
	return e.Uid
}

// occurrenceStamp formats an occurrence's start the way eventID does.
func occurrenceStamp(e gocal.Event, start time.Time) string {
	if isAllDay(e) {
		return domain.CivilDateFromTime(start.In(time.UTC)).String()
	}
	return start.UTC().Format(time.RFC3339)
}

func toDomainEventWithNotes(calendarID string, e gocal.Event) domain.Event {
	if isAllDay(e) {
		start := domain.CivilDateFromTime(e.Start.In(time.UTC))
		end := start.AddDays(1)
		if e.End != nil {
			endUTC := e.End.In(time.UTC)
			d := domain.CivilDateFromTime(endUTC)
			// gocal reports the end as the last second of the final day
			// (inclusive); our end date is exclusive. An exact midnight is
			// already exclusive.
			if !endUTC.Equal(time.Date(endUTC.Year(), endUTC.Month(), endUTC.Day(), 0, 0, 0, 0, time.UTC)) {
				d = d.AddDays(1)
			}
			if start.Before(d) {
				end = d
			}
		}
		return domain.NewAllDayEvent(eventID(e.Uid, start.String()), calendarID, e.Summary, start, end, e.Location, e.Description)
	}

	end := *e.Start
	if e.End != nil {
		end = *e.End
	}
	return domain.NewTimedEvent(eventID(e.Uid, e.Start.UTC().Format(time.RFC3339)), calendarID, e.Summary, *e.Start, end, e.Location, e.Description)
}

func isAllDay(e gocal.Event) bool {
	return strings.EqualFold(e.RawStart.Params["VALUE"], "DATE") || len(e.RawStart.Value) == len("20060102")
}

// eventID keys an occurrence by UID plus start, since every expanded
// occurrence of a recurring event shares one UID.
func eventID(uid, start string) string {
	return uid + "/" + start
}
