// Package app is clical's application layer: the one API every front-end
// (the CLI in cmd/clical, the TUI in internal/ui, or any future UI) uses to
// work with calendars. It owns how the store, vault, providers and sync
// engine fit together, so front-ends only gather input and present output.
//
// Nothing here may know about terminals, prompts or rendering: passphrases
// and URLs are passed in as plain values, and results come back as data.
// See docs/ARCHITECTURE.md.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/adrg/xdg"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/engine"
	"github.com/miphe/clical/internal/providers/google"
	"github.com/miphe/clical/internal/providers/ical"
	"github.com/miphe/clical/internal/secrets"
	"github.com/miphe/clical/internal/store"
	"github.com/miphe/clical/internal/write"
)

// ErrLocked is returned by operations that need secrets (sync, adding,
// removing or changing a calendar) before Unlock has been called.
var ErrLocked = errors.New("app: vault is locked")

// Paths says where clical keeps its data.
type Paths struct {
	Vault string // directory of age-encrypted secrets
	DB    string // SQLite cache
	Log   string // log file; "" for none
	Write string // notes and tasks, see internal/write
}

// DefaultPaths is $XDG_DATA_HOME/clical (~/.local/share/clical), with the log
// in $XDG_STATE_HOME/clical (~/.local/state/clical).
func DefaultPaths() Paths {
	dir := filepath.Join(xdg.DataHome, "clical")
	return Paths{
		Vault: filepath.Join(dir, "vault"),
		DB:    filepath.Join(dir, "clical.db"),
		Log:   filepath.Join(xdg.StateHome, "clical", "clical.log"),
		Write: filepath.Join(dir, "write"),
	}
}

// CalendarInfo is a calendar together with the account it belongs to.
type CalendarInfo struct {
	Account  domain.Account
	Calendar domain.Calendar
}

// Label is "Account · Calendar", the name front-ends show for a calendar,
// using nicknames where set.
func (c CalendarInfo) Label() string {
	return c.Account.DisplayName() + " · " + c.Calendar.DisplayName()
}

// App is an open clical data directory. Reading calendars and events works
// right away; anything touching feed URLs needs Unlock first.
type App struct {
	paths  Paths
	store  *store.Store
	vault  *secrets.Vault
	engine *engine.Engine
	google *google.Provider

	writeOnce sync.Once // see write.go
	writes    *write.Store
	writeErr  error

	log     *slog.Logger // see log.go
	logFile io.Closer
	// failing is the calendars whose last sync failed (ID → true), so a
	// recovery can be logged.
	failMu  sync.Mutex
	failing map[string]bool
}

// Open opens the data directory, creating it if needed. Calendars added
// before color slots existed get one here.
func Open(p Paths) (*App, error) {
	s, err := store.Open(p.DB)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	log, logFile, err := openLog(p.Log)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("open log: %w", err)
	}
	a := &App{paths: p, store: s, log: log, logFile: logFile, failing: map[string]bool{}}
	if err := a.assignMissingSlots(context.Background()); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// Close releases the store and the log.
func (a *App) Close() error {
	a.logFile.Close()
	return a.store.Close()
}

// VaultExists reports whether a passphrase has already been chosen. When it
// hasn't, Unlock's passphrase becomes the new one — front-ends should ask
// for it twice.
func (a *App) VaultExists() (bool, error) {
	return secrets.Exists(a.paths.Vault)
}

// Unlock opens the vault with passphrase (creating it if it doesn't exist
// yet). It returns secrets.ErrWrongPassphrase for a wrong passphrase.
func (a *App) Unlock(passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase must not be empty")
	}
	v, err := secrets.Open(a.paths.Vault, func() (string, error) { return passphrase, nil })
	if err != nil {
		return err
	}
	provider := ical.New(func(calendarID string) (string, error) {
		b, err := v.LoadSecret(ical.SecretKey(calendarID))
		return string(b), err
	})
	a.vault = v
	a.google = google.New(a.googleConfig, vaultTokens{v})
	a.engine = engine.New(a.store, map[string]domain.CalendarProvider{"ical": provider, "google": a.google})
	return nil
}

// Calendars lists every calendar, ordered by account label then name.
func (a *App) Calendars(ctx context.Context) ([]CalendarInfo, error) {
	accounts, err := a.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.Account, len(accounts))
	for _, acct := range accounts {
		byID[acct.ID] = acct
	}
	cals, err := a.store.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CalendarInfo, 0, len(cals))
	for _, c := range cals {
		out = append(out, CalendarInfo{Account: byID[c.AccountID], Calendar: c})
	}
	return out, nil
}

// Events returns the cached events overlapping [from, to). It never touches
// the network; call Sync to refresh the cache.
func (a *App) Events(ctx context.Context, from, to time.Time) ([]domain.Event, error) {
	return a.store.EventsInRange(ctx, from, to)
}
