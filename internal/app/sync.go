package app

import (
	"context"
	"errors"

	"github.com/miphe/clical/internal/domain"
)

// SyncOutcome is the result of syncing one calendar.
type SyncOutcome struct {
	CalendarInfo
	// Changed is false when the feed had nothing new.
	Changed bool
	// Events is how many events the calendar holds after a full resync.
	Events int
	Err    error
}

// Unavailable returns the cause when the calendar's feed URL no longer works
// (reset or deleted) — fixed with SetCalendarURL, not by retrying.
func (o SyncOutcome) Unavailable() (*domain.CalendarUnavailableError, bool) {
	var u *domain.CalendarUnavailableError
	ok := errors.As(o.Err, &u)
	return u, ok
}

// Sync fetches every calendar's feed into the cache. A failing calendar
// doesn't stop the others; its outcome carries the error.
func (a *App) Sync(ctx context.Context) ([]SyncOutcome, error) {
	if a.engine == nil {
		return nil, ErrLocked
	}
	results, err := a.engine.SyncAll(ctx)
	if err != nil {
		a.log.Error("sync failed", "error", err.Error())
		return nil, err
	}
	out := make([]SyncOutcome, 0, len(results))
	for _, r := range results {
		out = append(out, a.outcome(SyncOutcome{
			CalendarInfo: CalendarInfo{Account: r.Account, Calendar: r.Calendar},
			Changed:      r.Changed,
			Events:       r.Events,
			Err:          r.Err,
		}))
	}
	return out, nil
}

// outcome logs o and returns it.
func (a *App) outcome(o SyncOutcome) SyncOutcome {
	a.failMu.Lock()
	recovered := o.Err == nil && a.failing[o.Calendar.ID]
	if o.Err != nil {
		a.failing[o.Calendar.ID] = true
	} else {
		delete(a.failing, o.Calendar.ID)
	}
	a.failMu.Unlock()
	a.logOutcome(o, recovered)
	return o
}

// NeedsSignIn reports whether the calendar's account must sign in again
// (its access was revoked or expired).
func (o SyncOutcome) NeedsSignIn() bool {
	var r *domain.ReauthRequiredError
	return errors.As(o.Err, &r)
}

// SyncCalendar syncs one calendar — e.g. one just added — without touching
// the others.
func (a *App) SyncCalendar(ctx context.Context, cal CalendarInfo) SyncOutcome {
	if a.engine == nil {
		return SyncOutcome{CalendarInfo: cal, Err: ErrLocked}
	}
	r := a.engine.SyncOne(ctx, cal.Account, cal.Calendar)
	return a.outcome(SyncOutcome{CalendarInfo: cal, Changed: r.Changed, Events: r.Events, Err: r.Err})
}
