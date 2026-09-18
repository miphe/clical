// Package secrets stores OAuth tokens in an age-encrypted, passphrase-keyed
// vault. One file per account, so refreshing one account's token never
// touches another's, and the passphrase itself is cached only in memory for
// the lifetime of the process that opened the vault.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"golang.org/x/oauth2"
)

// ErrWrongPassphrase is returned by Open when the supplied passphrase can't
// decrypt a token already in the vault.
var ErrWrongPassphrase = errors.New("secrets: wrong passphrase")

const tokenExt = ".age"

// Vault is an open, unlocked token store. The derived key lives only in
// memory for the lifetime of the Vault.
type Vault struct {
	dir       string
	recipient *age.ScryptRecipient
	identity  *age.ScryptIdentity
}

// Open unlocks (or initializes) the vault at dir using a passphrase obtained
// from prompt. dir is created with 0700 permissions if it doesn't exist yet.
// If the vault already holds at least one account's token, the passphrase is
// validated by attempting to decrypt it; a mismatch returns ErrWrongPassphrase.
func Open(dir string, prompt func() (string, error)) (*Vault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create vault dir: %w", err)
	}

	passphrase, err := prompt()
	if err != nil {
		return nil, fmt.Errorf("secrets: read passphrase: %w", err)
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive vault key: %w", err)
	}
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive vault key: %w", err)
	}

	v := &Vault{dir: dir, recipient: recipient, identity: identity}

	existing, err := v.anyExistingAccountID()
	if err != nil {
		return nil, fmt.Errorf("secrets: read vault dir: %w", err)
	}
	if existing != "" {
		if _, err := v.LoadToken(existing); err != nil {
			return nil, ErrWrongPassphrase
		}
	}

	return v, nil
}

func (v *Vault) anyExistingAccountID() (string, error) {
	entries, err := os.ReadDir(v.dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), tokenExt) {
			return strings.TrimSuffix(e.Name(), tokenExt), nil
		}
	}
	return "", nil
}

func (v *Vault) path(accountID string) string {
	return filepath.Join(v.dir, accountID+tokenExt)
}

// SaveToken encrypts and persists tok for accountID, replacing any existing
// token for that account.
func (v *Vault) SaveToken(accountID string, tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("secrets: marshal token: %w", err)
	}

	f, err := os.OpenFile(v.path(accountID), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("secrets: open token file: %w", err)
	}
	defer f.Close()

	w, err := age.Encrypt(f, v.recipient)
	if err != nil {
		return fmt.Errorf("secrets: encrypt token: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("secrets: write token: %w", err)
	}
	return w.Close()
}

// LoadToken decrypts and returns the stored token for accountID.
func (v *Vault) LoadToken(accountID string) (*oauth2.Token, error) {
	f, err := os.Open(v.path(accountID))
	if err != nil {
		return nil, fmt.Errorf("secrets: open token file: %w", err)
	}
	defer f.Close()

	r, err := age.Decrypt(f, v.identity)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt token: %w", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("secrets: read token: %w", err)
	}

	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("secrets: unmarshal token: %w", err)
	}
	return &tok, nil
}

// DeleteToken removes accountID's token, if any.
func (v *Vault) DeleteToken(accountID string) error {
	if err := os.Remove(v.path(accountID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("secrets: delete token: %w", err)
	}
	return nil
}
