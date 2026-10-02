package ical

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apognu/gocal"

	"github.com/miphe/clical/internal/domain"
)

// parse reads an iCalendar feed and returns every non-cancelled event
// overlapping [from, to), with recurring events expanded into one Event per
// occurrence.
func parse(r io.Reader, calendarID string, from, to time.Time) ([]domain.Event, error) {
	p := gocal.NewParser(r)
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
	notes, links := describe(e)
	e.Description = notes
	return toDomainEventWithNotes(calendarID, e).WithLinks(links)
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
