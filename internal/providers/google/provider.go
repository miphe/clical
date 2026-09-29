// Package google holds the Google OAuth onboarding flow. It's shelved while
// iCal secret URLs (internal/providers/ical) are the primary integration, and
// is kept only as the fallback for calendars whose Workspace admin has
// disabled secret addresses. It does not implement domain.CalendarProvider.
package google

import (
	"golang.org/x/oauth2"
	oauth2google "golang.org/x/oauth2/google"

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

// Provider runs Google OAuth for a single client configuration. Tokens for
// authenticated accounts are persisted directly to vault and never returned
// to the caller.
type Provider struct {
	cfg   OAuthConfig
	vault *secrets.Vault

	// promptURL is called with the consent URL the user must open. openBrowser
	// additionally attempts to open it automatically; its error is ignored.
	promptURL   func(url string)
	openBrowser func(url string) error
}

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
