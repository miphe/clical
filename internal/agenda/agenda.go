// Package agenda groups events into local calendar days. It's pure — no I/O —
// so both the plain-text `clical agenda` command and the TUI's Agenda view
// can use it.
package agenda

import (
	"sort"
	"time"

	"github.com/miphe/clical/internal/domain"
)

// Item is one event as it appears on one day. Start and End are in the
// agenda's location; both are zero for all-day events.
type Item struct {
	Event      domain.Event
	Start, End time.Time
}

// Day is one local calendar day and the events touching it.
type Day struct {
	Date  domain.CivilDate
	Items []Item
}

// Build lays out events over days consecutive days starting at from, in
// loc. An event appears on every day it overlaps: all-day events on each
// date in [start, end), timed events on each local day their [start, end)
// intersects (zero-length events on the day they start). Within a day,
// all-day events come first, then timed events by start time, then title.
func Build(events []domain.Event, from domain.CivilDate, days int, loc *time.Location) []Day {
	out := make([]Day, days)
	for i := range out {
		out[i].Date = from.AddDays(i)
	}

	for _, e := range events {
		if ds, de, ok := e.DateRange(); ok {
			for i := range out {
				if d := out[i].Date; !d.Before(ds) && d.Before(de) {
					out[i].Items = append(out[i].Items, Item{Event: e})
				}
			}
			continue
		}

		ts, te, _ := e.TimeRange()
		start, end := ts.In(loc), te.In(loc)
		for i := range out {
			dayStart := out[i].Date.In(loc)
			dayEnd := out[i].Date.AddDays(1).In(loc)
			overlaps := start.Before(dayEnd) && end.After(dayStart)
			zeroInDay := start.Equal(end) && !start.Before(dayStart) && start.Before(dayEnd)
			if overlaps || zeroInDay {
				out[i].Items = append(out[i].Items, Item{Event: e, Start: start, End: end})
			}
		}
	}

	for i := range out {
		items := out[i].Items
		sort.SliceStable(items, func(a, b int) bool {
			ia, ib := items[a], items[b]
			if ia.Event.AllDay != ib.Event.AllDay {
				return ia.Event.AllDay
			}
			if !ia.Start.Equal(ib.Start) {
				return ia.Start.Before(ib.Start)
			}
			return ia.Event.Title < ib.Event.Title
		})
	}
	return out
}

// TimeLabel renders the item's time as seen on date: "all day", or
// "HH:MM–HH:MM" with "…" where the event continues from or into another day,
// "24:00" for an end at midnight, and a single time for zero-length events.
func (it Item) TimeLabel(date domain.CivilDate, loc *time.Location) string {
	if it.Event.AllDay {
		return "all day"
	}
	dayStart, dayEnd := date.In(loc), date.AddDays(1).In(loc)
	from := "…"
	if !it.Start.Before(dayStart) {
		from = it.Start.Format("15:04")
	}
	to := "…"
	switch {
	case it.End.Equal(dayEnd):
		to = "24:00"
	case !it.End.After(dayEnd):
		to = it.End.Format("15:04")
	}
	if it.Start.Equal(it.End) {
		return from
	}
	return from + "–" + to
}

// WeekStart returns the first day of the week containing d, for weeks
// starting on first (time.Monday for ISO weeks).
func WeekStart(d domain.CivilDate, first time.Weekday) domain.CivilDate {
	back := (int(d.Weekday()) - int(first) + 7) % 7
	return d.AddDays(-back)
}

// MonthGrid lays out d's month as whole weeks starting on first: the grid's
// first day (which may be in the previous month) and how many weeks (4–6) it
// takes to cover the month.
func MonthGrid(d domain.CivilDate, first time.Weekday) (start domain.CivilDate, weeks int) {
	firstOfMonth := domain.NewCivilDate(d.Year, d.Month, 1)
	start = WeekStart(firstOfMonth, first)
	lead := (int(firstOfMonth.Weekday()) - int(first) + 7) % 7
	weeks = (lead + d.DaysInMonth() + 6) / 7
	return start, weeks
}
