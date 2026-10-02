package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/providers/ical"
)

// Feed is an iCal feed URL that CheckFeed has normalized and fetched
// successfully. It can only be obtained from CheckFeed, so AddCalendar and
// SetCalendarURL never store an unchecked URL.
type Feed struct {
	url string
	// Events is how many occurrences fall in the sync window.
	Events int
}

// CheckFeed normalizes rawURL (webcal:// → https://, https only) and fetches
// it once. A URL the server rejects comes back as
// *domain.CalendarUnavailableError. Works without Unlock.
func (a *App) CheckFeed(ctx context.Context, rawURL string) (Feed, error) {
	u, err := ical.NormalizeURL(rawURL)
	if err != nil {
		return Feed{}, err
	}
	n, err := ical.New(nil).Probe(ctx, u)
	if err != nil {
		return Feed{}, err
	}
	return Feed{url: u, Events: n}, nil
}

// AddCalendar stores feed as calendar name under the account labelled
// account (created if new). Adding the same feed again updates its name.
func (a *App) AddCalendar(ctx context.Context, account, name string, feed Feed) (CalendarInfo, error) {
	if a.vault == nil {
		return CalendarInfo{}, ErrLocked
	}
	account, name = domain.SafeText(account), domain.SafeText(name)
	if strings.TrimSpace(account) == "" || strings.TrimSpace(name) == "" {
		return CalendarInfo{}, errors.New("account and calendar name must not be empty")
	}
	existing, err := a.Calendars(ctx)
	if err != nil {
		return CalendarInfo{}, err
	}
	acct := domain.Account{ID: domain.NewAccountID("ical", account), Provider: "ical", Label: account}
	for _, c := range existing {
		// An account is found by its nickname too, so naming the account
		// as the sidebar shows it adds to it rather than making another.
		if c.Account.Provider == "ical" && (strings.EqualFold(c.Account.Label, account) || strings.EqualFold(c.Account.Nickname, account)) {
			acct = c.Account
			break
		}
	}
	cal := domain.Calendar{ID: ical.CalendarID(feed.url), AccountID: acct.ID, Name: name}
	cal.ColorSlot = slotFor(existing, cal)

	if err := a.vault.SaveSecret(ical.SecretKey(cal.ID), []byte(feed.url)); err != nil {
		return CalendarInfo{}, fmt.Errorf("save feed URL: %w", err)
	}
	if err := a.store.UpsertAccount(ctx, acct); err != nil {
		return CalendarInfo{}, err
	}
	if err := a.store.UpsertCalendar(ctx, cal); err != nil {
		return CalendarInfo{}, err
	}
	// Re-adding a feed under a name means calling it that name.
	if err := a.store.SetCalendarNickname(ctx, cal, ""); err != nil {
		return CalendarInfo{}, err
	}
	return CalendarInfo{Account: acct, Calendar: cal}, nil
}

// slotFor keeps cal's slot if it already exists (re-adding a feed), else
// picks the lowest slot no other calendar uses, so a removed calendar's color
// is reused before new ones are introduced.
func slotFor(existing []CalendarInfo, cal domain.Calendar) int {
	used := map[int]bool{}
	for _, c := range existing {
		if c.Calendar.ID == cal.ID && c.Calendar.AccountID == cal.AccountID && c.Calendar.ColorSlot > 0 {
			return c.Calendar.ColorSlot
		}
		used[c.Calendar.ColorSlot] = true
	}
	slot := 1
	for used[slot] {
		slot++
	}
	return slot
}

// assignMissingSlots gives every calendar without a color slot one, in list
// order.
func (a *App) assignMissingSlots(ctx context.Context) error {
	cals, err := a.Calendars(ctx)
	if err != nil {
		return err
	}
	for i, c := range cals {
		if c.Calendar.ColorSlot > 0 {
			continue
		}
		c.Calendar.ColorSlot = slotFor(cals, c.Calendar)
		if err := a.store.UpsertCalendar(ctx, c.Calendar); err != nil {
			return err
		}
		cals[i] = c
	}
	return nil
}

// NoCalendarError means FindCalendar matched nothing.
type NoCalendarError struct{ Ref string }

func (e *NoCalendarError) Error() string {
	return fmt.Sprintf("no calendar matches %q", e.Ref)
}

// AmbiguousCalendarError means FindCalendar matched more than one calendar.
type AmbiguousCalendarError struct {
	Ref     string
	Matches []CalendarInfo
}

func (e *AmbiguousCalendarError) Error() string {
	names := make([]string, 0, len(e.Matches))
	for _, m := range e.Matches {
		names = append(names, fmt.Sprintf("%s (%s)", m.Label(), m.Calendar.ID))
	}
	return fmt.Sprintf("%q matches several calendars: %s", e.Ref, strings.Join(names, ", "))
}

// FindCalendar resolves ref — a calendar ID, or a case-insensitive name or
// nickname — to exactly one calendar, optionally only within the account
// named (by label or nickname) account.
func (a *App) FindCalendar(ctx context.Context, ref, account string) (CalendarInfo, error) {
	cals, err := a.Calendars(ctx)
	if err != nil {
		return CalendarInfo{}, err
	}
	return matchCalendar(cals, ref, account)
}

func matchCalendar(cals []CalendarInfo, ref, account string) (CalendarInfo, error) {
	var matches []CalendarInfo
	for _, c := range cals {
		if account != "" && !strings.EqualFold(c.Account.Label, account) && !strings.EqualFold(c.Account.Nickname, account) {
			continue
		}
		if c.Calendar.ID == ref || strings.EqualFold(c.Calendar.Name, ref) || strings.EqualFold(c.Calendar.Nickname, ref) {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return CalendarInfo{}, &NoCalendarError{Ref: ref}
	case 1:
		return matches[0], nil
	default:
		return CalendarInfo{}, &AmbiguousCalendarError{Ref: ref, Matches: matches}
	}
}

// RemoveCalendar deletes cal, its cached events and its stored URL, and its
// account if that leaves the account empty.
func (a *App) RemoveCalendar(ctx context.Context, cal CalendarInfo) error {
	if a.vault == nil {
		return ErrLocked
	}
	// Store first: a leftover encrypted secret is harmless, but a calendar
	// whose secret is gone would fail every sync.
	if err := a.store.DeleteCalendar(ctx, cal.Calendar); err != nil {
		return err
	}
	if cal.Account.Provider == "google" {
		// A Google account's sign-in is shared by its calendars; drop it with
		// the last one.
		rest, err := a.Calendars(ctx)
		if err != nil {
			return err
		}
		for _, c := range rest {
			if c.Account.ID == cal.Account.ID {
				return nil
			}
		}
		if err := a.vault.DeleteToken(cal.Account.ID); err != nil {
			return fmt.Errorf("calendar removed, but deleting the account's sign-in failed: %w", err)
		}
		return nil
	}
	if err := a.vault.DeleteSecret(ical.SecretKey(cal.Calendar.ID)); err != nil {
		return fmt.Errorf("calendar removed, but deleting its stored URL failed: %w", err)
	}
	return nil
}

// SetCalendarURL replaces cal's feed URL, e.g. after it was reset. The
// calendar keeps its ID (still derived from the original URL), and its next
// sync is a full one.
func (a *App) SetCalendarURL(ctx context.Context, cal CalendarInfo, feed Feed) error {
	if a.vault == nil {
		return ErrLocked
	}
	if cal.Account.Provider != "ical" {
		return errors.New("only iCal calendars have a feed URL")
	}
	if err := a.vault.SaveSecret(ical.SecretKey(cal.Calendar.ID), []byte(feed.url)); err != nil {
		return fmt.Errorf("save feed URL: %w", err)
	}
	return a.store.ResetSyncToken(ctx, cal.Calendar)
}
