// Package secrets stores small secrets (iCal feed URLs, OAuth tokens) in an
// age-encrypted, passphrase-keyed vault. One file per key, so updating one
// secret never touches another, and the passphrase itself is cached only in
// memory for the lifetime of the process that opened the vault.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"filippo.io/age"
	"golang.org/x/oauth2"
)

// ErrWrongPassphrase is returned by Open when the supplied passphrase can't
// decrypt a secret already in the vault.
var ErrWrongPassphrase = errors.New("secrets: wrong passphrase")

const secretExt = ".age"

var validKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Vault is an open, unlocked secret store. The derived key lives only in
// memory for the lifetime of the Vault.
type Vault struct {
	dir       string
	recipient *age.ScryptRecipient
	identity  *age.ScryptIdentity
}

// Open unlocks (or initializes) the vault at dir using a passphrase obtained
// from prompt. dir is created with 0700 permissions if it doesn't exist yet.
// If the vault already holds at least one secret, the passphrase is validated
// by attempting to decrypt it; a mismatch returns ErrWrongPassphrase.
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

	existing, err := v.anyExistingKey()
	if err != nil {
		return nil, fmt.Errorf("secrets: read vault dir: %w", err)
	}
	if existing != "" {
		if _, err := v.LoadSecret(existing); err != nil {
			return nil, ErrWrongPassphrase
		}
	}

	return v, nil
}

// Exists reports whether a vault with at least one secret exists at dir —
// i.e. whether a passphrase has already been chosen for it.
func Exists(dir string) (bool, error) {
	key, err := anyKey(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("secrets: read vault dir: %w", err)
	}
	return key != "", nil
}

func (v *Vault) anyExistingKey() (string, error) {
	return anyKey(v.dir)
}

func anyKey(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), secretExt) {
			return strings.TrimSuffix(e.Name(), secretExt), nil
		}
	}
	return "", nil
}

func (v *Vault) path(key string) (string, error) {
	if !validKey.MatchString(key) {
		return "", fmt.Errorf("secrets: invalid key %q", key)
	}
	return filepath.Join(v.dir, key+secretExt), nil
}

// SaveSecret encrypts and persists data under key, replacing any existing
// secret with that key. Keys are limited to [A-Za-z0-9_-] so they are always
// safe as file names.
func (v *Vault) SaveSecret(key string, data []byte) error {
	p, err := v.path(key)
	if err != nil {
		return err
	}

	// Write to a temporary file and rename it into place, so a failed or
	// interrupted write never destroys the secret it was replacing.
	f, err := os.CreateTemp(v.dir, "."+key+"-*.tmp")
	if err != nil {
		return fmt.Errorf("secrets: create secret file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()

	w, err := age.Encrypt(f, v.recipient)
	if err != nil {
		return fmt.Errorf("secrets: encrypt: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("secrets: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("secrets: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("secrets: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("secrets: write: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("secrets: save secret file: %w", err)
	}
	// Sync the directory too, so the rename itself survives a crash.
	if err := syncDir(v.dir); err != nil {
		return fmt.Errorf("secrets: save secret file: %w", err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// LoadSecret decrypts and returns the secret stored under key.
func (v *Vault) LoadSecret(key string) ([]byte, error) {
	p, err := v.path(key)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("secrets: open secret file: %w", err)
	}
	defer f.Close()

	r, err := age.Decrypt(f, v.identity)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("secrets: read: %w", err)
	}
	return data, nil
}

// DeleteSecret removes the secret stored under key, if any.
func (v *Vault) DeleteSecret(key string) error {
	p, err := v.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("secrets: delete: %w", err)
	}
	return nil
}

// SaveToken stores an OAuth token under accountID.
func (v *Vault) SaveToken(accountID string, tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("secrets: marshal token: %w", err)
	}
	return v.SaveSecret(accountID, data)
}

// LoadToken returns the OAuth token stored under accountID.
func (v *Vault) LoadToken(accountID string) (*oauth2.Token, error) {
	data, err := v.LoadSecret(accountID)
	if err != nil {
		return nil, err
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("secrets: unmarshal token: %w", err)
	}
	return &tok, nil
}

// DeleteToken removes accountID's OAuth token, if any.
func (v *Vault) DeleteToken(accountID string) error {
	return v.DeleteSecret(accountID)
}
