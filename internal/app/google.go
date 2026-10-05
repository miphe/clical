package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/oauth2"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/providers/google"
	"github.com/miphe/clical/internal/secrets"
)

// Google accounts sign in with the user's own OAuth client (a "Desktop app"
// client in their Google Cloud project), stored in the vault. Each account's
// token is stored in the vault under the account's ID.

const googleClientKey = "google-oauth-client"

// ErrGoogleClientRejected means Google refused the OAuth client ID or secret
// while finishing a sign-in.
var ErrGoogleClientRejected = google.ErrClientRejected

// GoogleClient is an OAuth client's ID and secret, as typed in.
type GoogleClient struct {
	ID, Secret string
}

// ValidateGoogleClient checks the shape of a typed-in client (Google checks
// the rest when signing in) and returns it trimmed.
func ValidateGoogleClient(c GoogleClient) (GoogleClient, error) {
	c.ID, c.Secret = strings.TrimSpace(c.ID), strings.TrimSpace(c.Secret)
	if !strings.HasSuffix(c.ID, ".apps.googleusercontent.com") {
		return c, errors.New("that doesn't look like an OAuth client ID (it ends in .apps.googleusercontent.com)")
	}
	if c.Secret == "" {
		return c, errors.New("the client secret is required")
	}
	return c, nil
}

// SavedGoogleClientID is the ID of the OAuth client saved in the vault, or ""
// if none has been saved yet. The secret is never handed out.
func (a *App) SavedGoogleClientID() (string, error) {
	if a.vault == nil {
		return "", ErrLocked
	}
	_, err := a.vault.LoadSecret(googleClientKey)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	cfg, err := a.googleConfig()
	return cfg.ClientID, err
}

// SetGoogleClient validates and stores the OAuth client's ID and secret.
func (a *App) SetGoogleClient(c GoogleClient) error {
	if a.vault == nil {
		return ErrLocked
	}
	c, err := ValidateGoogleClient(c)
	if err != nil {
		return err
	}
	b, err := json.Marshal(google.OAuthConfig{ClientID: c.ID, ClientSecret: c.Secret})
	if err != nil {
		return err
	}
	return a.vault.SaveSecret(googleClientKey, b)
}

func (a *App) googleConfig() (google.OAuthConfig, error) {
	if a.vault == nil {
		return google.OAuthConfig{}, ErrLocked
	}
	b, err := a.vault.LoadSecret(googleClientKey)
	if err != nil {
		return google.OAuthConfig{}, errors.New("no Google OAuth client set up yet")
	}
	var cfg google.OAuthConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return google.OAuthConfig{}, fmt.Errorf("read the Google OAuth client: %w", err)
	}
	return cfg, nil
}

// vaultTokens stores OAuth tokens in the vault.
type vaultTokens struct{ v *secrets.Vault }

func (t vaultTokens) LoadToken(id string) (*oauth2.Token, error)   { return t.v.LoadToken(id) }
func (t vaultTokens) SaveToken(id string, tok *oauth2.Token) error { return t.v.SaveToken(id, tok) }

// SignIn is a browser sign-in in progress: show or open URL(), then Wait.
type SignIn interface {
	URL() string
	Wait(ctx context.Context) (domain.Account, error)
	Cancel()
}

type googleSignIn struct {
	url    string
	wait   func(ctx context.Context) (domain.Account, *oauth2.Token, error)
	cancel func()
	app    *App
	expect string
	// client is a typed-in client that isn't saved yet; it's saved once a
	// sign-in with it succeeds.
	client *GoogleClient
}

// BeginGoogleSignIn starts signing in a Google account. expect, if set, is
// the account that must sign in (when signing an account in again); Google's
// page preselects it. client, if set, is a typed-in OAuth client to use
// instead of the saved one; it's only saved if the sign-in succeeds, so a
// mistyped client is never kept.
func (a *App) BeginGoogleSignIn(expect string, client *GoogleClient) (SignIn, error) {
	if a.vault == nil {
		return nil, ErrLocked
	}
	var cfg google.OAuthConfig
	if client != nil {
		c, err := ValidateGoogleClient(*client)
		if err != nil {
			return nil, err
		}
		client = &c
		cfg = google.OAuthConfig{ClientID: c.ID, ClientSecret: c.Secret}
	} else {
		var err error
		if cfg, err = a.googleConfig(); err != nil {
			return nil, err
		}
	}
	s, err := google.BeginSignIn(cfg, expect)
	if err != nil {
		return nil, err
	}
	return &googleSignIn{url: s.URL, wait: s.Wait, cancel: s.Close, app: a, expect: expect, client: client}, nil
}

// URL is the address to open in a browser.
func (g *googleSignIn) URL() string { return g.url }

// Wait finishes the sign-in once the browser comes back, and stores the
// account's token. The account itself is only saved once calendars are
// added (AddGoogleCalendars).
func (g *googleSignIn) Wait(ctx context.Context) (domain.Account, error) {
	acct, tok, err := g.wait(ctx)
	if err != nil {
		g.app.log.Warn("Google sign-in failed", "error", err.Error())
		return domain.Account{}, err
	}
	if g.expect != "" && !strings.EqualFold(acct.Label, g.expect) {
		return domain.Account{}, fmt.Errorf("signed in as %s, but this account is %s", acct.Label, g.expect)
	}
	if g.client != nil {
		// Google accepted it: now it's worth keeping.
		if err := g.app.SetGoogleClient(*g.client); err != nil {
			return domain.Account{}, fmt.Errorf("save the OAuth client: %w", err)
		}
	}
	if err := g.app.vault.SaveToken(acct.ID, tok); err != nil {
		return domain.Account{}, fmt.Errorf("save the sign-in: %w", err)
	}
	g.app.log.Info("signed in to Google", "account", acct.Label)
	return acct, nil
}

// Cancel abandons the sign-in; a typed-in client is discarded with it.
func (g *googleSignIn) Cancel() { g.cancel() }

// RemoteCalendar is a calendar in a Google account, as offered for adding.
type RemoteCalendar struct {
	ID      string // Google's calendar ID
	Name    string
	Primary bool
	// FreeBusyOnly calendars show only "busy" blocks, without titles.
	FreeBusyOnly bool
	// Added is true when the calendar is already in clical.
	Added bool
}

// GoogleCalendars lists a signed-in account's calendars, including ones
// shared with it, primary first.
func (a *App) GoogleCalendars(ctx context.Context, acct domain.Account) ([]RemoteCalendar, error) {
	if a.google == nil {
		return nil, ErrLocked
	}
	remote, err := a.google.ListCalendars(ctx, acct.ID)
	if err != nil {
		return nil, err
	}
	existing, err := a.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	added := map[string]bool{}
	for _, c := range existing {
		added[c.Calendar.ID] = true
	}
	out := make([]RemoteCalendar, 0, len(remote))
	for _, r := range remote {
		out = append(out, RemoteCalendar{
			ID: r.ID, Name: r.Name, Primary: r.Primary,
			FreeBusyOnly: r.AccessRole == "freeBusyReader",
			Added:        added[google.CalendarID(acct.ID, r.ID)],
		})
	}
	return out, nil
}

// AddGoogleCalendars saves acct and the chosen calendars (each with a color
// slot). Calendars already in clical keep theirs.
func (a *App) AddGoogleCalendars(ctx context.Context, acct domain.Account, picks []RemoteCalendar) ([]CalendarInfo, error) {
	if a.vault == nil {
		return nil, ErrLocked
	}
	if len(picks) == 0 {
		return nil, errors.New("choose at least one calendar")
	}
	if err := a.store.UpsertAccount(ctx, acct); err != nil {
		return nil, err
	}
	var out []CalendarInfo
	for _, p := range picks {
		existing, err := a.Calendars(ctx)
		if err != nil {
			return nil, err
		}
		cal := domain.Calendar{ID: google.CalendarID(acct.ID, p.ID), AccountID: acct.ID, Name: p.Name}
		cal.ColorSlot = slotFor(existing, cal)
		if err := a.store.UpsertCalendar(ctx, cal); err != nil {
			return nil, err
		}
		out = append(out, CalendarInfo{Account: acct, Calendar: cal})
	}
	for _, c := range out {
		a.log.Info("calendar added", "calendar", c.Label())
	}
	return out, nil
}
