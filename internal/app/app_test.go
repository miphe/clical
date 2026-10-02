package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/providers/ical"
	"github.com/miphe/clical/internal/secrets"
)

func openTest(t *testing.T) (*App, Paths) {
	t.Helper()
	dir := t.TempDir()
	p := Paths{Vault: filepath.Join(dir, "vault"), DB: filepath.Join(dir, "clical.db")}
	a, err := Open(p)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a, p
}

// feed builds a Feed directly; outside tests only CheckFeed can.
func feed(url string) Feed { return Feed{url: url, Events: 1} }

func TestLockedOperationsRefuse(t *testing.T) {
	a, _ := openTest(t)
	ctx := context.Background()
	cal := CalendarInfo{Calendar: domain.Calendar{ID: "c", AccountID: "a"}}

	if _, err := a.Sync(ctx); !errors.Is(err, ErrLocked) {
		t.Errorf("Sync() error = %v, want ErrLocked", err)
	}
	if _, err := a.AddCalendar(ctx, "Personal", "Main", feed("https://x.test/a.ics")); !errors.Is(err, ErrLocked) {
		t.Errorf("AddCalendar() error = %v, want ErrLocked", err)
	}
	if err := a.RemoveCalendar(ctx, cal); !errors.Is(err, ErrLocked) {
		t.Errorf("RemoveCalendar() error = %v, want ErrLocked", err)
	}
	if err := a.SetCalendarURL(ctx, cal, feed("https://x.test/b.ics")); !errors.Is(err, ErrLocked) {
		t.Errorf("SetCalendarURL() error = %v, want ErrLocked", err)
	}
}

func TestUnlock(t *testing.T) {
	a, p := openTest(t)
	if err := a.Unlock(""); err == nil {
		t.Error("Unlock(\"\") error = nil, want error")
	}
	if ok, _ := a.VaultExists(); ok {
		t.Error("VaultExists() = true before any secret was saved")
	}
	if err := a.Unlock("pw"); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if _, err := a.AddCalendar(context.Background(), "Personal", "Main", feed("https://x.test/a.ics")); err != nil {
		t.Fatalf("AddCalendar() error = %v", err)
	}
	if ok, _ := a.VaultExists(); !ok {
		t.Error("VaultExists() = false after saving a secret")
	}

	b, err := Open(p)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer b.Close()
	if err := b.Unlock("wrong"); !errors.Is(err, secrets.ErrWrongPassphrase) {
		t.Errorf("Unlock(wrong) error = %v, want ErrWrongPassphrase", err)
	}
}

func TestCalendarLifecycle(t *testing.T) {
	a, p := openTest(t)
	ctx := context.Background()
	if err := a.Unlock("pw"); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}

	added, err := a.AddCalendar(ctx, "Personal", "Main", feed("https://x.test/main.ics"))
	if err != nil {
		t.Fatalf("AddCalendar() error = %v", err)
	}
	if added.Label() != "Personal · Main" || added.Calendar.ID != ical.CalendarID("https://x.test/main.ics") {
		t.Errorf("AddCalendar() = %+v", added)
	}
	// Re-adding the same feed renames rather than duplicates.
	if _, err := a.AddCalendar(ctx, "Personal", "Primary", feed("https://x.test/main.ics")); err != nil {
		t.Fatalf("re-AddCalendar() error = %v", err)
	}
	cals, _ := a.Calendars(ctx)
	if len(cals) != 1 || cals[0].Calendar.Name != "Primary" {
		t.Fatalf("Calendars() = %+v, want one calendar named Primary", cals)
	}

	found, err := a.FindCalendar(ctx, "primary", "")
	if err != nil || found.Calendar.ID != added.Calendar.ID {
		t.Fatalf("FindCalendar() = %+v, %v", found, err)
	}

	if err := a.SetCalendarURL(ctx, found, feed("https://x.test/new.ics")); err != nil {
		t.Fatalf("SetCalendarURL() error = %v", err)
	}
	v, err := secrets.Open(p.Vault, func() (string, error) { return "pw", nil })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := v.LoadSecret(ical.SecretKey(found.Calendar.ID)); string(b) != "https://x.test/new.ics" {
		t.Errorf("stored URL after SetCalendarURL = %q", b)
	}

	if err := a.RemoveCalendar(ctx, found); err != nil {
		t.Fatalf("RemoveCalendar() error = %v", err)
	}
	if cals, _ := a.Calendars(ctx); len(cals) != 0 {
		t.Errorf("Calendars() after remove = %+v, want none", cals)
	}
	if _, err := v.LoadSecret(ical.SecretKey(found.Calendar.ID)); err == nil {
		t.Error("feed URL still in the vault after RemoveCalendar")
	}
}

func TestMatchCalendar(t *testing.T) {
	personal := domain.Account{ID: "p", Label: "Personal"}
	work := domain.Account{ID: "w", Label: "Work"}
	cals := []CalendarInfo{
		{personal, domain.Calendar{ID: "id-main", AccountID: "p", Name: "Main"}},
		{work, domain.Calendar{ID: "id-team", AccountID: "w", Name: "Team"}},
		{personal, domain.Calendar{ID: "id-holp", AccountID: "p", Name: "Holidays"}},
		{work, domain.Calendar{ID: "id-holw", AccountID: "w", Name: "Holidays"}},
	}

	for _, c := range []struct{ ref, account, wantID string }{
		{"main", "", "id-main"},
		{"id-team", "", "id-team"},
		{"Holidays", "work", "id-holw"},
	} {
		got, err := matchCalendar(cals, c.ref, c.account)
		if err != nil || got.Calendar.ID != c.wantID {
			t.Errorf("matchCalendar(%q, %q) = %q, %v; want %q", c.ref, c.account, got.Calendar.ID, err, c.wantID)
		}
	}

	var amb *AmbiguousCalendarError
	if _, err := matchCalendar(cals, "Holidays", ""); !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Errorf("ambiguous name error = %v, want AmbiguousCalendarError with 2 matches", err)
	}
	var none *NoCalendarError
	for _, c := range []struct{ ref, account string }{{"nope", ""}, {"Team", "Personal"}} {
		if _, err := matchCalendar(cals, c.ref, c.account); !errors.As(err, &none) {
			t.Errorf("matchCalendar(%q, %q) error = %v, want NoCalendarError", c.ref, c.account, err)
		}
	}
}

func TestCheckFeedRejectsNonHTTPS(t *testing.T) {
	a, _ := openTest(t)
	if _, err := a.CheckFeed(context.Background(), "http://x.test/a.ics"); err == nil {
		t.Error("CheckFeed(http://) error = nil, want rejection")
	}
}

func TestColorSlots(t *testing.T) {
	a, p := openTest(t)
	ctx := context.Background()
	if err := a.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	add := func(account, name, url string) CalendarInfo {
		t.Helper()
		c, err := a.AddCalendar(ctx, account, name, feed(url))
		if err != nil {
			t.Fatalf("AddCalendar(%s) error = %v", name, err)
		}
		return c
	}

	one := add("Work", "Zeta", "https://x.test/1.ics")
	two := add("Personal", "Alpha", "https://x.test/2.ics") // sorts first in the list
	three := add("Personal", "Beta", "https://x.test/3.ics")
	if one.Calendar.ColorSlot != 1 || two.Calendar.ColorSlot != 2 || three.Calendar.ColorSlot != 3 {
		t.Fatalf("slots = %d %d %d, want 1 2 3 in order of adding", one.Calendar.ColorSlot, two.Calendar.ColorSlot, three.Calendar.ColorSlot)
	}

	// Re-adding a feed keeps its slot.
	if again := add("Personal", "Alpha renamed", "https://x.test/2.ics"); again.Calendar.ColorSlot != 2 {
		t.Errorf("re-added feed slot = %d, want 2", again.Calendar.ColorSlot)
	}

	// A removed calendar's slot is reused before a new one is introduced.
	if err := a.RemoveCalendar(ctx, two); err != nil {
		t.Fatal(err)
	}
	if four := add("Personal", "Gamma", "https://x.test/4.ics"); four.Calendar.ColorSlot != 2 {
		t.Errorf("slot after removal = %d, want the freed 2", four.Calendar.ColorSlot)
	}

	// Calendars from before slots existed are backfilled on Open.
	if err := a.store.UpsertCalendar(ctx, domain.Calendar{ID: "legacy", AccountID: one.Account.ID, Name: "Legacy"}); err != nil {
		t.Fatal(err)
	}
	b, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	cals, _ := b.Calendars(ctx)
	seen := map[int]string{}
	for _, c := range cals {
		if c.Calendar.ColorSlot == 0 {
			t.Errorf("%s has no slot after Open", c.Calendar.Name)
		}
		if other, dup := seen[c.Calendar.ColorSlot]; dup {
			t.Errorf("%s and %s share slot %d", c.Calendar.Name, other, c.Calendar.ColorSlot)
		}
		seen[c.Calendar.ColorSlot] = c.Calendar.Name
	}
}

func TestAddCalendarRequiresNames(t *testing.T) {
	a, _ := openTest(t)
	if err := a.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddCalendar(context.Background(), " ", "Main", feed("https://x.test/a.ics")); err == nil {
		t.Error("AddCalendar with an empty account error = nil")
	}
}
