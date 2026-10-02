package main

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/engine"
	"github.com/miphe/clical/internal/providers/ical"
	"github.com/miphe/clical/internal/store"
	"github.com/miphe/clical/internal/ui"
)

// TUICmd opens the full-screen calendar. It's the default command.
type TUICmd struct{}

func (c *TUICmd) Run() error {
	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	cals, err := db.ListCalendars(context.Background())
	if err != nil {
		return err
	}
	if len(cals) == 0 {
		fmt.Println("No calendars yet. Add one with `clical calendars add --account <group> --name <name>`.")
		return nil
	}

	// Unlocked once, before the TUI takes over the terminal; the passphrase
	// then lives in memory for the session.
	vault, err := unlockVault()
	if err != nil {
		return err
	}
	provider := ical.New(func(calendarID string) (string, error) {
		b, err := vault.LoadSecret(ical.SecretKey(calendarID))
		return string(b), err
	})
	src := storeSource{db: db, engine: engine.New(db, map[string]domain.CalendarProvider{"ical": provider})}

	_, err = tea.NewProgram(ui.New(src, time.Local, time.Now())).Run()
	return err
}

// storeSource adapts the store and sync engine to ui.Source.
type storeSource struct {
	db     *store.Store
	engine *engine.Engine
}

func (s storeSource) Calendars(ctx context.Context) ([]ui.CalendarRow, error) {
	accounts, err := s.db.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}
	cals, err := s.db.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]ui.CalendarRow, 0, len(cals))
	for _, c := range cals {
		rows = append(rows, ui.CalendarRow{Account: byID[c.AccountID], Calendar: c})
	}
	return rows, nil
}

func (s storeSource) Events(ctx context.Context, from, to time.Time) ([]domain.Event, error) {
	return s.db.EventsInRange(ctx, from, to)
}

func (s storeSource) Sync(ctx context.Context) ([]engine.Result, error) {
	return s.engine.SyncAll(ctx)
}
