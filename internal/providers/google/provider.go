// Package google implements domain.CalendarProvider against the Google
// Calendar API.
package google

import (
	"context"
	"errors"

	"golang.org/x/oauth2"
	oauth2google "golang.org/x/oauth2/google"

	"github.com/miphe/clical/internal/domain"
	"github.com/miphe/clical/internal/secrets"
)

// ReadOnlyScope is the OAuth scope requested for Phase 1 (read-only).
const ReadOnlyScope = "https://www.googleapis.com/auth/calendar.readonly"

// OAuthConfig holds the BYO OAuth client credentials a user registers in
// their own Google Cloud project (Desktop app type) — see README for setup.
// This is not treated as fully confidential the way access/refresh tokens
// are, and is supplied via plain config, not the encrypted vault.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// Provider implements domain.CalendarProvider against the Google Calendar
// API for a single OAuth client configuration. Tokens for authenticated
// accounts are persisted directly to vault; the CalendarProvider interface
// never exposes a raw token to its caller.
type Provider struct {
	cfg   OAuthConfig
	vault *secrets.Vault

	// promptURL is called with the consent URL the user must open. openBrowser
	// additionally attempts to open it automatically; its error is ignored.
	promptURL   func(url string)
	openBrowser func(url string) error
}

var _ domain.CalendarProvider = (*Provider)(nil)

// New builds a Provider. vault is used to persist tokens obtained during
// Authenticate and to refresh tokens transparently during later calls.
func New(cfg OAuthConfig, vault *secrets.Vault, promptURL func(url string)) *Provider {
	return &Provider{
		cfg:         cfg,
		vault:       vault,
		promptURL:   promptURL,
		openBrowser: defaultOpenBrowser,
	}
}

func (p *Provider) oauth2Config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     oauth2google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{ReadOnlyScope, "openid", "email"},
	}
}

// ListCalendars is implemented in calendars.go, added in milestone M2.
func (p *Provider) ListCalendars(ctx context.Context, acct domain.Account) ([]domain.Calendar, error) {
	return nil, errors.New("google: ListCalendars not implemented yet")
}

// SyncEvents is implemented in events.go, added in milestone M3.
func (p *Provider) SyncEvents(ctx context.Context, cal domain.Calendar, token string) (domain.SyncResult, error) {
	return domain.SyncResult{}, errors.New("google: SyncEvents not implemented yet")
}
