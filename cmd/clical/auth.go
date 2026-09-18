package main

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/miphe/clical/internal/providers/google"
	"github.com/miphe/clical/internal/secrets"
	"github.com/miphe/clical/internal/store"
)

// AuthCmd groups account-management subcommands.
type AuthCmd struct {
	Add  AuthAddCmd  `cmd:"" help:"Authenticate a new Google account."`
	List AuthListCmd `cmd:"" help:"List connected accounts."`
}

// AuthAddCmd runs the OAuth loopback flow for a Google account created with
// the user's own Google Cloud OAuth client (see README for setup).
type AuthAddCmd struct {
	ClientID     string `required:"" help:"OAuth client ID from your Google Cloud project."`
	ClientSecret string `required:"" help:"OAuth client secret from your Google Cloud project."`
}

func (c *AuthAddCmd) Run() error {
	passphrase, err := promptPassphrase("Vault passphrase: ")
	if err != nil {
		return err
	}
	vault, err := secrets.Open(vaultDir(), fixedPassphrase(passphrase))
	if err != nil {
		return fmt.Errorf("open vault: %w", err)
	}

	provider := google.New(
		google.OAuthConfig{ClientID: c.ClientID, ClientSecret: c.ClientSecret},
		vault,
		func(url string) { fmt.Fprintf(os.Stderr, "Open this URL to authorize clical:\n\n  %s\n\n", url) },
	)

	acct, err := provider.Authenticate(context.Background())
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}

	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	if err := db.UpsertAccount(context.Background(), acct); err != nil {
		return fmt.Errorf("save account: %w", err)
	}

	fmt.Printf("Authenticated %s (%s)\n", acct.Label, acct.ID)
	return nil
}

// AuthListCmd lists every connected account and its token expiry.
type AuthListCmd struct{}

func (c *AuthListCmd) Run() error {
	db, err := store.Open(dbPath())
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	accounts, err := db.ListAccounts(context.Background())
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	if len(accounts) == 0 {
		fmt.Println("No accounts. Run `clical auth add` to connect one.")
		return nil
	}

	passphrase, err := promptPassphrase("Vault passphrase: ")
	if err != nil {
		return err
	}
	vault, err := secrets.Open(vaultDir(), fixedPassphrase(passphrase))
	if err != nil {
		return fmt.Errorf("open vault: %w", err)
	}

	for _, acct := range accounts {
		tok, err := vault.LoadToken(acct.ID)
		if err != nil {
			fmt.Printf("%s\t%s\t(token unreadable: %v)\n", acct.Provider, acct.Label, err)
			continue
		}
		fmt.Printf("%s\t%s\ttoken expires %s\n", acct.Provider, acct.Label, tok.Expiry.Local())
	}
	return nil
}

func fixedPassphrase(passphrase string) func() (string, error) {
	return func() (string, error) { return passphrase, nil }
}

func promptPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	return string(b), nil
}
