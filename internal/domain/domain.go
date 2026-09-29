// Package domain holds provider-agnostic calendar types. Nothing in this
// package may import a concrete provider.
package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Account identifies one connected calendar account.
type Account struct {
	ID       string // stable id, e.g. sha256("provider:label")[:16] hex
	Provider string // "google", ...
	Label    string // e.g. the account's email address
}

// NewAccountID derives a stable, deterministic account ID from a provider
// name and label (e.g. the account's email address). Deterministic so
// re-authenticating an already-known account is detected as the same
// account rather than creating a duplicate, while the ID itself doesn't
// expose the label in vault filenames or logs.
func NewAccountID(provider, label string) string {
	sum := sha256.Sum256([]byte(provider + ":" + label))
	return hex.EncodeToString(sum[:])[:16]
}

// Calendar is one calendar within an Account.
type Calendar struct {
	ID        string
	AccountID string
	Name      string
	Color     string // hex, e.g. "#3B82F6"
}

// CivilDate is a timezone-less calendar date. All-day events are represented
// with CivilDate, never time.Time, so a UTC-midnight conversion can't shift
// the date by a day depending on the viewer's timezone.
type CivilDate struct {
	Year  int
	Month int
	Day   int
}

// NewCivilDate constructs a CivilDate from its components.
func NewCivilDate(year, month, day int) CivilDate {
	return CivilDate{Year: year, Month: month, Day: day}
}

// CivilDateFromTime takes the year/month/day of t as observed in t's own
// location. Callers choose the location before calling this.
func CivilDateFromTime(t time.Time) CivilDate {
	y, m, d := t.Date()
	return CivilDate{Year: y, Month: int(m), Day: d}
}

// Before reports whether d is strictly earlier than o.
func (d CivilDate) Before(o CivilDate) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}

// AddDays returns the date n days after d (n may be negative).
func (d CivilDate) AddDays(n int) CivilDate {
	t := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	return CivilDateFromTime(t)
}

// ParseCivilDate parses a "YYYY-MM-DD" date as produced by String.
func ParseCivilDate(s string) (CivilDate, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return CivilDate{}, fmt.Errorf("domain: parse date %q: %w", s, err)
	}
	return CivilDateFromTime(t), nil
}

// In returns midnight at the start of d in loc.
func (d CivilDate) In(loc *time.Location) time.Time {
	return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, loc)
}

// String renders d as "YYYY-MM-DD".
func (d CivilDate) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// Event is a single calendar event. Exactly one of the (start, end) /
// (startDate, endDate) pairs is meaningful, selected by AllDay; use the
// constructors and TimeRange/DateRange accessors rather than the fields
// directly so that invariant can't be gotten wrong at a call site.
type Event struct {
	ID         string
	CalendarID string
	Title      string
	Location   string
	Notes      string
	AllDay     bool

	start, end         time.Time
	startDate, endDate CivilDate
}

// NewTimedEvent builds a timed (non-all-day) event. start/end are converted
// to UTC for storage; render in the local zone at display time.
func NewTimedEvent(id, calendarID, title string, start, end time.Time, location, notes string) Event {
	return Event{
		ID:         id,
		CalendarID: calendarID,
		Title:      title,
		Location:   location,
		Notes:      notes,
		AllDay:     false,
		start:      start.UTC(),
		end:        end.UTC(),
	}
}

// NewAllDayEvent builds an all-day event spanning [start, end) as civil dates.
func NewAllDayEvent(id, calendarID, title string, start, end CivilDate, location, notes string) Event {
	return Event{
		ID:         id,
		CalendarID: calendarID,
		Title:      title,
		Location:   location,
		Notes:      notes,
		AllDay:     true,
		startDate:  start,
		endDate:    end,
	}
}

// TimeRange returns the event's start/end for a timed event. ok is false for
// an all-day event.
func (e Event) TimeRange() (start, end time.Time, ok bool) {
	if e.AllDay {
		return time.Time{}, time.Time{}, false
	}
	return e.start, e.end, true
}

// DateRange returns the event's start/end civil dates for an all-day event.
// ok is false for a timed event.
func (e Event) DateRange() (start, end CivilDate, ok bool) {
	if !e.AllDay {
		return CivilDate{}, CivilDate{}, false
	}
	return e.startDate, e.endDate, true
}

// SyncResult is the outcome of one incremental (or full) sync pass over a
// single calendar.
type SyncResult struct {
	Upserted   []Event
	DeletedIDs []string
	NextToken  string
	// FullResync is true when Upserted is a complete snapshot of the
	// calendar (e.g. after a 410 Gone forced a full resync) rather than a
	// partial delta. When true, DeletedIDs is not populated — the store
	// should replace all events for the calendar instead.
	FullResync bool
}

// CalendarProvider is the seam every calendar backend (iCal feeds, Google,
// CalDAV, ...) implements. Everything above this layer speaks only in domain
// types. Onboarding (logging in, discovering or adding calendars) differs too
// much between backends to share an interface, so it lives in each
// provider's own package and CLI flow.
type CalendarProvider interface {
	// SyncEvents fetches a delta for cal. token is the opaque sync cursor
	// persisted from the previous call's SyncResult.NextToken; "" means
	// full sync.
	SyncEvents(ctx context.Context, cal Calendar, token string) (SyncResult, error)
}
