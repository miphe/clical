package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/miphe/clical/internal/app"
)

// CalendarsCmd groups calendar-management subcommands.
type CalendarsCmd struct {
	Add    CalendarsAddCmd    `cmd:"" help:"Add a calendar from its secret iCal URL."`
	List   CalendarsListCmd   `cmd:"" help:"List added calendars."`
	Remove CalendarsRemoveCmd `cmd:"" help:"Remove a calendar and its cached events."`
	Rename CalendarsRenameCmd `cmd:"" help:"Give a calendar your own name."`
	SetURL CalendarsSetURLCmd `cmd:"" name:"set-url" help:"Replace a calendar's secret iCal URL, e.g. after resetting it."`
}

// calendarRef selects a calendar by ID or by name, optionally narrowed to one
// account.
type calendarRef struct {
	Calendar string `arg:"" help:"Calendar name, nickname or ID (see 'calendars list')."`
	Account  string `help:"Only match calendars in this account (name or nickname)."`
}

func (r calendarRef) resolve(ctx context.Context, a *app.App) (app.CalendarInfo, error) {
	cal, err := a.FindCalendar(ctx, r.Calendar, r.Account)
	var none *app.NoCalendarError
	var ambiguous *app.AmbiguousCalendarError
	switch {
	case errors.As(err, &none):
		return cal, fmt.Errorf("%w; see 'clical calendars list'", err)
	case errors.As(err, &ambiguous):
		return cal, fmt.Errorf("%w; use the ID or --account", err)
	}
	return cal, err
}

// promptFeed reads a feed URL from a hidden prompt (so it stays out of shell
// history) and checks it, turning a rejected URL into advice.
func promptFeed(ctx context.Context, a *app.App, prompt string) (app.Feed, error) {
	raw, err := promptHidden(prompt)
	if err != nil {
		return app.Feed{}, err
	}
	fmt.Fprintln(os.Stderr, "Checking feed...")
	feed, err := a.CheckFeed(ctx, raw)
	if u, ok := (app.SyncOutcome{Err: err}).Unavailable(); ok {
		return app.Feed{}, fmt.Errorf("that URL doesn't work (%s); check you copied the whole secret address, or that it hasn't been reset", u.Reason)
	}
	if err != nil {
		return app.Feed{}, fmt.Errorf("feed check failed: %w", err)
	}
	return feed, nil
}

// CalendarsAddCmd adds an iCal feed.
type CalendarsAddCmd struct {
	Account string `required:"" help:"Group to put the calendar under, e.g. Personal or Work."`
	Name    string `required:"" help:"Display name for the calendar."`
}

func (c *CalendarsAddCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()

	feed, err := promptFeed(ctx, a, "Secret iCal URL: ")
	if err != nil {
		return err
	}
	if err := unlock(a); err != nil {
		return err
	}
	cal, err := a.AddCalendar(ctx, c.Account, c.Name, feed)
	if err != nil {
		return err
	}
	fmt.Printf("Added %s (%d events in the sync window)\n", cal.Label(), feed.Events)
	return nil
}

// CalendarsListCmd lists added calendars. No passphrase needed.
type CalendarsListCmd struct{}

func (c *CalendarsListCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()

	cals, err := a.Calendars(context.Background())
	if err != nil {
		return err
	}
	if len(cals) == 0 {
		fmt.Println(noCalendarsHint)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tCALENDAR\tID")
	for _, cal := range cals {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			withOwnName(cal.Account.DisplayName(), cal.Account.Label),
			withOwnName(cal.Calendar.DisplayName(), cal.Calendar.Name),
			cal.Calendar.ID)
	}
	return w.Flush()
}

// withOwnName adds a nicknamed item's own name, e.g. "Family (Main)".
func withOwnName(shown, own string) string {
	if shown == own {
		return shown
	}
	return shown + " (" + own + ")"
}

// CalendarsRenameCmd gives a calendar the user's own name. No passphrase
// needed.
type CalendarsRenameCmd struct {
	calendarRef
	Name string `arg:"" optional:"" help:"New name. Leave out to use the calendar's own name again."`
}

func (c *CalendarsRenameCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()

	cal, err := c.resolve(ctx, a)
	if err != nil {
		return err
	}
	before := cal.Label()
	renamed, err := a.RenameCalendar(ctx, cal, c.Name)
	if err != nil {
		return err
	}
	fmt.Printf("Renamed %s to %s\n", before, renamed.Label())
	return nil
}

// AccountsCmd groups account-management subcommands.
type AccountsCmd struct {
	Rename AccountsRenameCmd `cmd:"" help:"Give an account your own name."`
}

// AccountsRenameCmd gives an account the user's own name. No passphrase
// needed.
type AccountsRenameCmd struct {
	Account string `arg:"" help:"Account name, nickname or ID."`
	Name    string `arg:"" optional:"" help:"New name. Leave out to use the account's own name again."`
}

func (c *AccountsRenameCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()

	acct, err := a.FindAccount(ctx, c.Account)
	if err != nil {
		return err
	}
	before := acct.DisplayName()
	renamed, err := a.RenameAccount(ctx, acct, c.Name)
	if err != nil {
		return err
	}
	fmt.Printf("Renamed %s to %s\n", before, renamed.DisplayName())
	return nil
}

// CalendarsRemoveCmd deletes a calendar, its cached events and its stored URL.
type CalendarsRemoveCmd struct {
	calendarRef
	Yes bool `help:"Don't ask for confirmation."`
}

func (c *CalendarsRemoveCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()

	cal, err := c.resolve(ctx, a)
	if err != nil {
		return err
	}
	if !c.Yes {
		ok, err := confirm(fmt.Sprintf("Remove %s?", cal.Label()))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Not removed.")
			return nil
		}
	}
	if err := unlock(a); err != nil {
		return err
	}
	if err := a.RemoveCalendar(ctx, cal); err != nil {
		return err
	}
	fmt.Printf("Removed %s\n", cal.Label())
	return nil
}

// CalendarsSetURLCmd replaces a calendar's feed URL.
type CalendarsSetURLCmd struct {
	calendarRef
}

func (c *CalendarsSetURLCmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()

	cal, err := c.resolve(ctx, a)
	if err != nil {
		return err
	}
	feed, err := promptFeed(ctx, a, "New secret iCal URL: ")
	if err != nil {
		return err
	}
	if err := unlock(a); err != nil {
		return err
	}
	if err := a.SetCalendarURL(ctx, cal, feed); err != nil {
		return err
	}
	fmt.Printf("Updated %s (%d events in the sync window). Run 'clical sync' to refresh.\n", cal.Label(), feed.Events)
	return nil
}
