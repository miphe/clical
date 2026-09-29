package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/providers/ical"
	"github.com/miphe/clical/internal/store"
)

// CalendarsCmd groups calendar-management subcommands.
type CalendarsCmd struct {
	Add    CalendarsAddCmd    `cmd:"" help:"Add a calendar from its secret iCal URL."`
	List   CalendarsListCmd   `cmd:"" help:"List added calendars."`
	Remove CalendarsRemoveCmd `cmd:"" help:"Remove a calendar and its cached events."`
	SetURL CalendarsSetURLCmd `cmd:"" name:"set-url" help:"Replace a calendar's secret iCal URL, e.g. after resetting it."`
}

// calendarRef selects a calendar by ID or by name, optionally narrowed to one
// account.
type calendarRef struct {
	Calendar string `arg:"" help:"Calendar name or ID (see 'calendars list')."`
	Account  string `help:"Only match calendars in this account."`
}

// resolve finds the single calendar matching the reference.
func (r calendarRef) resolve(ctx context.Context, db *store.Store) (domain.Account, domain.Calendar, error) {
	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		return domain.Account{}, domain.Calendar{}, err
	}
	cals, err := db.ListCalendars(ctx)
	if err != nil {
		return domain.Account{}, domain.Calendar{}, err
	}
	return matchCalendar(accounts, cals, r.Calendar, r.Account)
}

func matchCalendar(accounts []domain.Account, cals []domain.Calendar, ref, account string) (domain.Account, domain.Calendar, error) {
	byID := make(map[string]domain.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}

	var matches []domain.Calendar
	for _, c := range cals {
		if account != "" && !strings.EqualFold(byID[c.AccountID].Label, account) {
			continue
		}
		if c.ID == ref || strings.EqualFold(c.Name, ref) {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return domain.Account{}, domain.Calendar{}, fmt.Errorf("no calendar matches %q; see 'clical calendars list'", ref)
	case 1:
		return byID[matches[0].AccountID], matches[0], nil
	default:
		var names []string
		for _, c := range matches {
			names = append(names, fmt.Sprintf("%s · %s (%s)", byID[c.AccountID].Label, c.Name, c.ID))
		}
		return domain.Account{}, domain.Calendar{}, fmt.Errorf("%q matches several calendars: %s; use the ID or --account",
			ref, strings.Join(names, ", "))
	}
}

// CalendarsRemoveCmd deletes a calendar, its cached events and its stored URL.
type CalendarsRemoveCmd struct {
	calendarRef
	Yes bool `help:"Don't ask for confirmation."`
}

func (c *CalendarsRemoveCmd) Run() error {
	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	acct, cal, err := c.resolve(ctx, db)
	if err != nil {
		return err
	}
	if !c.Yes {
		ok, err := confirm(fmt.Sprintf("Remove %s · %s?", acct.Label, cal.Name))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Not removed.")
			return nil
		}
	}

	vault, err := unlockVault()
	if err != nil {
		return err
	}
	// Store first: a leftover encrypted URL is harmless, a calendar whose URL
	// is gone would fail every sync.
	if err := db.DeleteCalendar(ctx, cal); err != nil {
		return err
	}
	if err := vault.DeleteSecret(ical.SecretKey(cal.ID)); err != nil {
		return fmt.Errorf("calendar removed, but deleting its stored URL failed: %w", err)
	}
	fmt.Printf("Removed %s · %s\n", acct.Label, cal.Name)
	return nil
}

// CalendarsSetURLCmd replaces a calendar's feed URL. The calendar keeps its
// ID, so it keeps its place and settings; the next sync fetches everything.
type CalendarsSetURLCmd struct {
	calendarRef
}

func (c *CalendarsSetURLCmd) Run() error {
	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	acct, cal, err := c.resolve(ctx, db)
	if err != nil {
		return err
	}

	raw, err := promptHidden("New secret iCal URL: ")
	if err != nil {
		return err
	}
	feedURL, err := ical.NormalizeURL(raw)
	if err != nil {
		return err
	}
	count, err := probeFeed(ctx, feedURL)
	if err != nil {
		return err
	}

	vault, err := unlockVault()
	if err != nil {
		return err
	}
	if err := vault.SaveSecret(ical.SecretKey(cal.ID), []byte(feedURL)); err != nil {
		return fmt.Errorf("save feed URL: %w", err)
	}
	if err := db.ResetSyncToken(ctx, cal); err != nil {
		return err
	}
	fmt.Printf("Updated %s · %s (%d events in the sync window). Run 'clical sync' to refresh.\n", acct.Label, cal.Name, count)
	return nil
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

	count, err := probeFeed(context.Background(), feedURL)
	if err != nil {
		return err
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

// probeFeed checks a feed URL before it's saved, turning a rejected URL into
// advice rather than a raw HTTP error.
func probeFeed(ctx context.Context, feedURL string) (int, error) {
	fmt.Fprintln(os.Stderr, "Checking feed...")
	count, err := ical.New(nil).Probe(ctx, feedURL)
	var unavailable *domain.CalendarUnavailableError
	if errors.As(err, &unavailable) {
		return 0, fmt.Errorf("that URL doesn't work (%s); check you copied the whole secret address, or that it hasn't been reset", unavailable.Reason)
	}
	if err != nil {
		return 0, fmt.Errorf("feed check failed: %w", err)
	}
	return count, nil
}
