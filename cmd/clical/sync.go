package main

import (
	"context"
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

	vault, err := unlockVault()
	if err != nil {
		return err
	}

	icalProvider := ical.New(func(calendarID string) (string, error) {
		b, err := vault.LoadSecret(ical.SecretKey(calendarID))
		return string(b), err
	})
	eng := engine.New(db, map[string]domain.CalendarProvider{"ical": icalProvider})

	results, err := eng.SyncAll(context.Background())
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No calendars. Run `clical calendars add --account <group> --name <name>` to add one.")
		return nil
	}

	failed := 0
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, r := range results {
		status := "unchanged"
		switch {
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
