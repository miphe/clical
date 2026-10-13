package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/write"
)

// Write is the user's notes and tasks (internal/write). They need no
// secrets, so they work while the vault is locked, and live in files of
// their own rather than the cache, which can be rebuilt from the calendars.

// Write opens the notes directory on first use.
func (a *App) Write() (*write.Store, error) {
	a.writeOnce.Do(func() {
		if a.paths.Write == "" {
			a.writeErr = fmt.Errorf("app: no directory for notes")
			return
		}
		a.writes, a.writeErr = write.Open(a.paths.Write)
	})
	return a.writes, a.writeErr
}

// EventScope is where an event's notes live, and the header that says which
// event they are about, from its calendar and times as they are now. loc is
// the zone to write the start time in.
func (a *App) EventScope(ctx context.Context, e domain.Event, loc *time.Location) (write.Ref, *write.Header) {
	ref := write.RefFor(e)
	h := &write.Header{Event: ref}
	if cals, err := a.Calendars(ctx); err == nil {
		for _, c := range cals {
			if c.Calendar.ID == e.CalendarID {
				h.Calendar = c.Calendar.DisplayName()
			}
		}
	}
	if start, _, ok := e.TimeRange(); ok {
		h.Start = start.In(loc).Format(time.RFC3339)
	} else if ds, _, ok := e.DateRange(); ok {
		h.Start = ds.String()
	}
	return ref, h
}

// NoEventError means FindEvent matched nothing.
type NoEventError struct{ Text string }

func (e *NoEventError) Error() string { return fmt.Sprintf("no event matches %q", e.Text) }

// AmbiguousEventError means FindEvent matched more than one event.
type AmbiguousEventError struct {
	Text    string
	Matches []domain.Event
}

func (e *AmbiguousEventError) Error() string {
	return fmt.Sprintf("%q matches %d events", e.Text, len(e.Matches))
}

// FindEvent resolves text to one cached event overlapping [from, to): the one
// whose key or ID is text, or whose title contains it, ignoring case. If
// several titles contain it but exactly one is equal to it, that one.
func (a *App) FindEvent(ctx context.Context, text string, from, to time.Time) (domain.Event, error) {
	events, err := a.Events(ctx, from, to)
	if err != nil {
		return domain.Event{}, err
	}
	return matchEvent(events, text)
}

func matchEvent(events []domain.Event, text string) (domain.Event, error) {
	text = strings.TrimSpace(text)
	var exact, partial []domain.Event
	for _, e := range events {
		switch {
		case e.Identity() == text || e.ID == text:
			return e, nil
		case strings.EqualFold(e.Title, text):
			exact = append(exact, e)
			partial = append(partial, e)
		case text != "" && strings.Contains(strings.ToLower(e.Title), strings.ToLower(text)):
			partial = append(partial, e)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(partial) == 1:
		return partial[0], nil
	case len(partial) == 0:
		return domain.Event{}, &NoEventError{Text: text}
	}
	return domain.Event{}, &AmbiguousEventError{Text: text, Matches: partial}
}

// The rest are what a front-end that keeps a Write list on screen needs: read
// a list, change one, move or archive a task. They are the Store's methods,
// reached through here so front-ends don't open the files themselves.

// WriteList reads a scope's list; the zero Ref is the global one.
func (a *App) WriteList(scope write.Ref) (write.List, error) {
	st, err := a.Write()
	if err != nil {
		return write.List{}, err
	}
	return st.Get(scope)
}

// WriteUpdate changes a scope's list, see write.Store.Update.
func (a *App) WriteUpdate(scope write.Ref, hdr *write.Header, fn func(*write.List) error) error {
	st, err := a.Write()
	if err != nil {
		return err
	}
	return st.Update(scope, hdr, fn)
}

// WriteMove takes a task to another scope, see write.Store.Move.
func (a *App) WriteMove(id string, to write.Ref, hdr *write.Header) error {
	st, err := a.Write()
	if err != nil {
		return err
	}
	return st.Move(id, to, hdr)
}

// WriteArchive files a checked task away, see write.Store.Archive.
func (a *App) WriteArchive(id string, now time.Time) error {
	st, err := a.Write()
	if err != nil {
		return err
	}
	_, err = st.Archive(id, now)
	return err
}

// WriteArchived lists the archived tasks, oldest first, see
// write.Store.Archived.
func (a *App) WriteArchived() ([]write.Archived, error) {
	st, err := a.Write()
	if err != nil {
		return nil, err
	}
	return st.Archived()
}
