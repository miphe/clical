package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
)

// SyncCmd fetches every calendar's feed into the local cache.
type SyncCmd struct{}

func (c *SyncCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()

	cals, err := a.Calendars(ctx)
	if err != nil {
		return err
	}
	if len(cals) == 0 {
		fmt.Println(noCalendarsHint)
		return nil
	}
	if err := unlock(a); err != nil {
		return err
	}

	outcomes, err := a.Sync(ctx)
	if err != nil {
		return err
	}
	failed := 0
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, o := range outcomes {
		status := "unchanged"
		if u, ok := o.Unavailable(); ok {
			failed++
			status = fmt.Sprintf("feed URL no longer works (%s): replace it with 'clical calendars set-url %s'",
				u.Reason, o.Calendar.ID)
		} else if o.Err != nil {
			failed++
			status = "error: " + o.Err.Error()
		} else if o.Changed {
			status = fmt.Sprintf("%d events", o.Events)
		}
		fmt.Fprintf(w, "%s\t%s\n", o.Label(), status)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d calendars failed to sync", failed, len(outcomes))
	}
	return nil
}
