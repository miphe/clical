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
	// Nickname is the user's own name for the account, shown instead of
	// Label; "" for none. Label stays as it is: it identifies the account
	// (a Google account's email address, matched on signing in again).
	Nickname string
}

// DisplayName is the name to show for the account.
func (a Account) DisplayName() string {
	if a.Nickname != "" {
		return a.Nickname
	}
	return a.Label
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
	Name      string // as added, or as the provider names it
	// Nickname is the user's own name for the calendar, shown instead of
	// Name; "" for none.
	Nickname string
	// ColorSlot is the calendar's permanent place in the color palette,
	// starting at 1; 0 means not assigned yet. Front-ends map slots to actual
	// colors, so a calendar keeps its color when others are added or removed.
	ColorSlot int
}

// DisplayName is the name to show for the calendar.
func (c Calendar) DisplayName() string {
	if c.Nickname != "" {
		return c.Nickname
	}
	return c.Name
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

// AddMonths returns the same day n months later (n may be negative), clamped
// to the last day of a shorter month: 31 Jan + 1 month is 28 (or 29) Feb.
func (d CivilDate) AddMonths(n int) CivilDate {
	first := time.Date(d.Year, time.Month(d.Month)+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	f := CivilDateFromTime(first)
	return NewCivilDate(f.Year, f.Month, min(d.Day, f.DaysInMonth()))
}

// DaysInMonth is the number of days in d's month.
func (d CivilDate) DaysInMonth() int {
	return time.Date(d.Year, time.Month(d.Month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Weekday is the day of the week d falls on.
func (d CivilDate) Weekday() time.Weekday {
	return d.In(time.UTC).Weekday()
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

	// Key identifies the event across reschedules, for things the user
	// attaches to it (see internal/write). ID can change when an event
	// moves: for iCal it includes the start time. "" means ID is stable and
	// is the key; see Identity. The value comes from a feed, so it is never
	// used as a file name.
	Key string

	start, end         time.Time
	startDate, endDate CivilDate

	// Links found in the event (conference link, documents, ...), in display
	// order: video calls first. Notes refer to them as "[1]", "[2]", ...
	Links []Link
}

// LinkKind says what a link is for.
type LinkKind int

const (
	LinkOther LinkKind = iota
	LinkVideoCall
	LinkDocument
)

// Link is a web address attached to an event. URL is always http or https.
type Link struct {
	URL   string
	Label string // human-readable, e.g. "Join Google Meet" or a link's text
	Kind  LinkKind
}

// WithKey returns e with its stable key set.
func (e Event) WithKey(key string) Event {
	e.Key = key
	return e
}

// Identity is the event's stable key: Key, or ID when the provider's IDs are
// already stable.
func (e Event) Identity() string {
	if e.Key != "" {
		return e.Key
	}
	return e.ID
}

// WithLinks returns e with links attached.
func (e Event) WithLinks(links []Link) Event {
	if len(links) == 0 {
		e.Links = nil
		return e
	}
	e.Links = make([]Link, len(links))
	for i, l := range links {
		l.Label = SafeText(l.Label)
		e.Links[i] = l
	}
	return e
}

// NewTimedEvent builds a timed (non-all-day) event. start/end are converted
// to UTC for storage; render in the local zone at display time.
func NewTimedEvent(id, calendarID, title string, start, end time.Time, location, notes string) Event {
	return Event{
		ID:         id,
		CalendarID: calendarID,
		Title:      SafeText(title),
		Location:   SafeText(location),
		Notes:      SafeText(notes),
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
		Title:      SafeText(title),
		Location:   SafeText(location),
		Notes:      SafeText(notes),
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
