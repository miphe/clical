package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/providers/ical"
	"github.com/miphe/clical/internal/store"
)

// CalendarsCmd groups calendar-management subcommands.
type CalendarsCmd struct {
	Add  CalendarsAddCmd  `cmd:"" help:"Add a calendar from its secret iCal URL."`
	List CalendarsListCmd `cmd:"" help:"List added calendars."`
}

// CalendarsAddCmd adds an iCal feed. The URL is read from a hidden prompt,
// not a flag, so it never lands in shell history.
type CalendarsAddCmd struct {
	Account string `required:"" help:"Group to put the calendar under, e.g. Personal or Work."`
	Name    string `required:"" help:"Display name for the calendar."`
}

func (c *CalendarsAddCmd) Run() error {
	raw, err := promptHidden("Secret iCal URL: ")
	if err != nil {
		return err
	}
	feedURL, err := ical.NormalizeURL(raw)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Checking feed...")
	count, err := ical.New(nil).Probe(context.Background(), feedURL)
	if err != nil {
		return fmt.Errorf("feed check failed: %w", err)
	}

	vault, err := unlockVault()
	if err != nil {
		return err
	}

	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	acct := domain.Account{ID: domain.NewAccountID("ical", c.Account), Provider: "ical", Label: c.Account}
	cal := domain.Calendar{ID: ical.CalendarID(feedURL), AccountID: acct.ID, Name: c.Name}

	if err := vault.SaveSecret(ical.SecretKey(cal.ID), []byte(feedURL)); err != nil {
		return fmt.Errorf("save feed URL: %w", err)
	}
	if err := db.UpsertAccount(ctx, acct); err != nil {
		return fmt.Errorf("save account: %w", err)
	}
	if err := db.UpsertCalendar(ctx, cal); err != nil {
		return fmt.Errorf("save calendar: %w", err)
	}

	fmt.Printf("Added %s / %s (%d events in the sync window)\n", acct.Label, cal.Name, count)
	return nil
}

// CalendarsListCmd lists added calendars. It reads only the store, so no
// passphrase is needed.
type CalendarsListCmd struct{}

func (c *CalendarsListCmd) Run() error {
	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	labels := make(map[string]string, len(accounts))
	for _, a := range accounts {
		labels[a.ID] = a.Label
	}

	cals, err := db.ListCalendars(ctx)
	if err != nil {
		return fmt.Errorf("list calendars: %w", err)
	}
	if len(cals) == 0 {
		fmt.Println("No calendars. Run `clical calendars add --account <group> --name <name>` to add one.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tCALENDAR\tID")
	for _, cal := range cals {
		fmt.Fprintf(w, "%s\t%s\t%s\n", labels[cal.AccountID], cal.Name, cal.ID)
	}
	return w.Flush()
}
