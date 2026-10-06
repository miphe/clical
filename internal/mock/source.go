package mock

import (
	"context"
	"errors"
	"time"

	"github.com/miphe/clical/internal/app"
	"github.com/miphe/clical/internal/domain"
)

// errMock is what anything that would change or reach for real data says.
var errMock = errors.New("not available with mock data")

// Source serves the mock calendars and events. It has the methods of the
// TUI's Source: the TUI asks for what it needs and gets generated answers, and
// everything that would touch an account or the network says no.
type Source struct {
	now    time.Time
	loc    *time.Location
	cals   []app.CalendarInfo
	events []domain.Event
}

func newSource(now time.Time, loc *time.Location, events []domain.Event) *Source {
	return &Source{now: now, loc: loc, cals: calendars(), events: events}
}

func (s *Source) Calendars(context.Context) ([]app.CalendarInfo, error) {
	return append([]app.CalendarInfo(nil), s.cals...), nil
}

// Events are those overlapping [from, to).
func (s *Source) Events(_ context.Context, from, to time.Time) ([]domain.Event, error) {
	var out []domain.Event
	for _, e := range s.events {
		if start, end, ok := e.TimeRange(); ok {
			if start.Before(to) && (end.After(from) || (start.Equal(end) && !start.Before(from))) {
				out = append(out, e)
			}
		} else if ds, de, ok := e.DateRange(); ok {
			if ds.In(s.loc).Before(to) && de.In(s.loc).After(from) {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// Sync takes a moment, like a sync does, and fails for the two calendars that
// are made to.
func (s *Source) Sync(ctx context.Context) ([]app.SyncOutcome, error) {
	select {
	case <-time.After(600 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	out := make([]app.SyncOutcome, 0, len(s.cals))
	for _, c := range s.cals {
		out = append(out, s.outcome(c))
	}
	return out, nil
}

func (s *Source) SyncCalendar(_ context.Context, c app.CalendarInfo) app.SyncOutcome {
	return s.outcome(c)
}

func (s *Source) outcome(c app.CalendarInfo) app.SyncOutcome {
	o := app.SyncOutcome{CalendarInfo: c}
	switch {
	case c.Calendar.ID == calLibrary:
		o.Err = &domain.CalendarUnavailableError{CalendarID: c.Calendar.ID, Reason: "HTTP 410 Gone (mock)"}
	case c.Account.ID == acctClub.ID:
		o.Err = &domain.ReauthRequiredError{AccountID: c.Account.ID, Err: errors.New("mock")}
	}
	return o
}

func (s *Source) CheckFeed(context.Context, string) (app.Feed, error) { return app.Feed{}, errMock }

func (s *Source) AddCalendar(context.Context, string, string, app.Feed) (app.CalendarInfo, error) {
	return app.CalendarInfo{}, errMock
}

func (s *Source) RemoveCalendar(context.Context, app.CalendarInfo) error { return errMock }

func (s *Source) SetCalendarURL(context.Context, app.CalendarInfo, app.Feed) error { return errMock }

func (s *Source) RenameCalendar(_ context.Context, c app.CalendarInfo, _ string) (app.CalendarInfo, error) {
	return c, errMock
}

func (s *Source) RenameAccount(_ context.Context, a domain.Account, _ string) (domain.Account, error) {
	return a, errMock
}

func (s *Source) SavedGoogleClientID() (string, error) { return "", nil }

func (s *Source) BeginGoogleSignIn(string, *app.GoogleClient) (app.SignIn, error) {
	return nil, errMock
}

func (s *Source) GoogleCalendars(context.Context, domain.Account) ([]app.RemoteCalendar, error) {
	return nil, errMock
}

func (s *Source) AddGoogleCalendars(context.Context, domain.Account, []app.RemoteCalendar) ([]app.CalendarInfo, error) {
	return nil, errMock
}
