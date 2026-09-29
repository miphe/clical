// Package engine runs provider syncs and applies their results to the store.
// It's the only place that connects providers to the store; the TUI will
// drive it through tea.Cmds.
package engine

import (
	"context"
	"fmt"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/store"
)

// Engine syncs calendars using the provider registered for each account's
// provider name.
type Engine struct {
	store     *store.Store
	providers map[string]domain.CalendarProvider
}

// New builds an Engine. providers is keyed by domain.Account.Provider.
func New(s *store.Store, providers map[string]domain.CalendarProvider) *Engine {
	return &Engine{store: s, providers: providers}
}

// Result is the outcome of syncing one calendar.
type Result struct {
	Account  domain.Account
	Calendar domain.Calendar
	// Changed is false when the provider reported nothing new.
	Changed bool
	// Events is the number of events stored after a full resync.
	Events int
	Err    error
}

// SyncOne syncs a single calendar and applies the result.
func (e *Engine) SyncOne(ctx context.Context, acct domain.Account, cal domain.Calendar) Result {
	res := Result{Account: acct, Calendar: cal}

	p, ok := e.providers[acct.Provider]
	if !ok {
		res.Err = fmt.Errorf("engine: no provider for %q", acct.Provider)
		return res
	}

	token, err := e.store.SyncToken(ctx, cal)
	if err != nil {
		res.Err = err
		return res
	}

	r, err := p.SyncEvents(ctx, cal, token)
	if err != nil {
		res.Err = err
		return res
	}
	if err := e.store.ApplySync(ctx, cal, r); err != nil {
		res.Err = err
		return res
	}

	res.Changed = r.FullResync || len(r.Upserted) > 0 || len(r.DeletedIDs) > 0
	if r.FullResync {
		res.Events = len(r.Upserted)
	}
	return res
}

// SyncAll syncs every stored calendar, one after another. A failure on one
// calendar doesn't stop the others; per-calendar errors are in the results.
func (e *Engine) SyncAll(ctx context.Context) ([]Result, error) {
	accounts, err := e.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}

	cals, err := e.store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(cals))
	for _, cal := range cals {
		results = append(results, e.SyncOne(ctx, byID[cal.AccountID], cal))
	}
	return results, nil
}
