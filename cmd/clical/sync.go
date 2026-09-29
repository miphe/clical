package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/engine"
	"github.com/miphe/clical/internal/providers/ical"
	"github.com/miphe/clical/internal/store"
)

// SyncCmd fetches every calendar's feed into the local cache.
type SyncCmd struct{}

func (c *SyncCmd) Run() error {
	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	cals, err := db.ListCalendars(ctx)
	if err != nil {
		return err
	}
	if len(cals) == 0 {
		fmt.Println("No calendars. Run `clical calendars add --account <group> --name <name>` to add one.")
		return nil
	}

	vault, err := unlockVault()
	if err != nil {
		return err
	}

	icalProvider := ical.New(func(calendarID string) (string, error) {
		b, err := vault.LoadSecret(ical.SecretKey(calendarID))
		return string(b), err
	})
	eng := engine.New(db, map[string]domain.CalendarProvider{"ical": icalProvider})

	results, err := eng.SyncAll(ctx)
	if err != nil {
		return err
	}

	failed := 0
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range results {
		status := "unchanged"
		var unavailable *domain.CalendarUnavailableError
		switch {
		case errors.As(r.Err, &unavailable):
			failed++
			status = fmt.Sprintf("feed URL no longer works (%s): replace it with 'clical calendars set-url %s'",
				unavailable.Reason, r.Calendar.ID)
		case r.Err != nil:
			failed++
			status = "error: " + r.Err.Error()
		case r.Changed:
			status = fmt.Sprintf("%d events", r.Events)
		}
		fmt.Fprintf(w, "%s · %s\t%s\n", r.Account.Label, r.Calendar.Name, status)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d calendars failed to sync", failed, len(results))
	}
	return nil
}
