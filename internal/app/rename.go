package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/store"
)

// Calendars and accounts can be given the user's own names (nicknames),
// shown everywhere instead of their own. Their own names stay as they are:
// a Google account's label is the email address signing in again is
// checked against, and an iCal account's ID is derived from its label.
// Renaming needs no secrets, so it works while the vault is locked.

// RenameCalendar gives cal the user's own name. An empty name, or the
// calendar's own name, removes the nickname so its own name shows again.
func (a *App) RenameCalendar(ctx context.Context, cal CalendarInfo, name string) (CalendarInfo, error) {
	nick := cleanName(name)
	if nick == cal.Calendar.Name {
		nick = ""
	}
	if err := a.store.SetCalendarNickname(ctx, cal.Calendar, nick); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return CalendarInfo{}, &NoCalendarError{Ref: cal.Label()}
		}
		return CalendarInfo{}, err
	}
	cal.Calendar.Nickname = nick
	return cal, nil
}

// RenameAccount gives acct the user's own name, like RenameCalendar.
func (a *App) RenameAccount(ctx context.Context, acct domain.Account, name string) (domain.Account, error) {
	nick := cleanName(name)
	if nick == acct.Label {
		nick = ""
	}
	if err := a.store.SetAccountNickname(ctx, acct.ID, nick); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.Account{}, &NoAccountError{Ref: acct.DisplayName()}
		}
		return domain.Account{}, err
	}
	acct.Nickname = nick
	return acct, nil
}

// cleanName makes a typed name fit on one line: control characters are
// dropped and runs of whitespace, line breaks included, become one space.
func cleanName(s string) string {
	return strings.Join(strings.Fields(domain.SafeText(s)), " ")
}

// NoAccountError means FindAccount matched nothing.
type NoAccountError struct{ Ref string }

func (e *NoAccountError) Error() string {
	return fmt.Sprintf("no account matches %q", e.Ref)
}

// FindAccount resolves ref — an account ID, or a case-insensitive label or
// nickname — to exactly one account.
func (a *App) FindAccount(ctx context.Context, ref string) (domain.Account, error) {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	var matches []domain.Account
	for _, acct := range accounts {
		if acct.ID == ref || strings.EqualFold(acct.Label, ref) || strings.EqualFold(acct.Nickname, ref) {
			matches = append(matches, acct)
		}
	}
	switch len(matches) {
	case 0:
		return domain.Account{}, &NoAccountError{Ref: ref}
	case 1:
		return matches[0], nil
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, fmt.Sprintf("%s (%s, %s)", m.DisplayName(), m.Provider, m.ID))
	}
	return domain.Account{}, fmt.Errorf("%q matches several accounts: %s; use the ID", ref, strings.Join(names, ", "))
}
