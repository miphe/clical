package app

import (
	"context"
	"errors"
	"testing"

	"github.com/miphe/clical/internal/domain"
)

func TestRenameCalendarAndAccount(t *testing.T) {
	unlocked, p := openTest(t)
	ctx := context.Background()
	if err := unlocked.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	main, err := unlocked.AddCalendar(ctx, "Personal", "Main", feed("https://x.test/main.ics"))
	if err != nil {
		t.Fatal(err)
	}
	// Renaming needs no secrets: this one is never unlocked.
	a, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	renamed, err := a.RenameCalendar(ctx, main, "  Family\n\tstuff ")
	if err != nil || renamed.Calendar.Nickname != "Family stuff" {
		t.Fatalf("RenameCalendar() = %+v, %v", renamed, err)
	}
	acct, err := a.RenameAccount(ctx, main.Account, "Home")
	if err != nil || acct.DisplayName() != "Home" || acct.Label != "Personal" {
		t.Fatalf("RenameAccount() = %+v, %v", acct, err)
	}
	cals, _ := a.Calendars(ctx)
	if len(cals) != 1 || cals[0].Label() != "Home · Family stuff" || cals[0].Calendar.Name != "Main" {
		t.Fatalf("Calendars() = %+v, want nicknames shown and own names kept", cals)
	}

	// Either name finds them.
	for _, ref := range [][2]string{{"family stuff", "home"}, {"main", "personal"}} {
		if c, err := a.FindCalendar(ctx, ref[0], ref[1]); err != nil || c.Calendar.ID != main.Calendar.ID {
			t.Errorf("FindCalendar(%q, %q) = %+v, %v", ref[0], ref[1], c, err)
		}
	}
	if got, err := a.FindAccount(ctx, "HOME"); err != nil || got.ID != main.Account.ID {
		t.Errorf("FindAccount(HOME) = %+v, %v", got, err)
	}
	if _, err := a.FindAccount(ctx, "Work"); !errors.As(err, new(*NoAccountError)) {
		t.Errorf("FindAccount(Work) error = %v", err)
	}

	// An empty name, or the calendar's own, removes the nickname.
	if c, _ := a.RenameCalendar(ctx, cals[0], ""); c.Calendar.Nickname != "" {
		t.Errorf("empty rename kept nickname %q", c.Calendar.Nickname)
	}
	if acct, _ := a.RenameAccount(ctx, acct, "Personal"); acct.Nickname != "" {
		t.Errorf("renaming to the own label kept nickname %q", acct.Nickname)
	}

	gone := CalendarInfo{Account: main.Account, Calendar: domain.Calendar{ID: "nope", AccountID: main.Account.ID}}
	if _, err := a.RenameCalendar(ctx, gone, "x"); !errors.As(err, new(*NoCalendarError)) {
		t.Errorf("renaming a missing calendar error = %v", err)
	}
}

func TestAddCalendarFindsAccountByNickname(t *testing.T) {
	a, _ := openTest(t)
	ctx := context.Background()
	if err := a.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	main, err := a.AddCalendar(ctx, "Personal", "Main", feed("https://x.test/main.ics"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RenameAccount(ctx, main.Account, "Home"); err != nil {
		t.Fatal(err)
	}
	added, err := a.AddCalendar(ctx, "home", "Birthdays", feed("https://x.test/bdays.ics"))
	if err != nil || added.Account.ID != main.Account.ID || added.Label() != "Home · Birthdays" {
		t.Fatalf("AddCalendar under the nickname = %+v, %v; want it in the same account", added, err)
	}
}

func TestReAddingClearsTheNickname(t *testing.T) {
	a, _ := openTest(t)
	ctx := context.Background()
	if err := a.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	main, _ := a.AddCalendar(ctx, "Personal", "Main", feed("https://x.test/main.ics"))
	if _, err := a.RenameCalendar(ctx, main, "Family"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddCalendar(ctx, "Personal", "Primary", feed("https://x.test/main.ics")); err != nil {
		t.Fatal(err)
	}
	if cals, _ := a.Calendars(ctx); len(cals) != 1 || cals[0].Calendar.DisplayName() != "Primary" {
		t.Errorf("Calendars() = %+v, want the re-added name shown", cals)
	}
}

func TestGoogleNicknamesSurviveReAdding(t *testing.T) {
	a, _ := openTest(t)
	ctx := context.Background()
	if err := a.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	acct := domain.Account{ID: domain.NewAccountID("google", "me@company.com"), Provider: "google", Label: "me@company.com"}
	pick := []RemoteCalendar{{ID: "team@group.calendar.google.com", Name: "Team"}}
	added, err := a.AddGoogleCalendars(ctx, acct, pick)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RenameCalendar(ctx, added[0], "Squad"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RenameAccount(ctx, acct, "Work"); err != nil {
		t.Fatal(err)
	}
	// Signing in again saves the account and calendars as Google has them.
	if _, err := a.AddGoogleCalendars(ctx, acct, pick); err != nil {
		t.Fatal(err)
	}
	if cals, _ := a.Calendars(ctx); len(cals) != 1 || cals[0].Label() != "Work · Squad" || cals[0].Account.Label != "me@company.com" {
		t.Errorf("Calendars() = %+v, want nicknames kept", cals)
	}
}
