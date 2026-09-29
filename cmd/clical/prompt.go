package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/miphe/clical/internal/secrets"
)

// promptHidden reads a line from the terminal without echoing it, for
// passphrases and secret URLs.
func promptHidden(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("a terminal is required to enter secrets")
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return string(b), nil
}

func fixedPassphrase(passphrase string) func() (string, error) {
	return func() (string, error) { return passphrase, nil }
}

// unlockVault prompts for the passphrase and opens the vault.
func unlockVault() (*secrets.Vault, error) {
	passphrase, err := promptHidden("Vault passphrase: ")
	if err != nil {
		return nil, err
	}
	vault, err := secrets.Open(vaultDir(), fixedPassphrase(passphrase))
	if err != nil {
		return nil, fmt.Errorf("open vault: %w", err)
	}
	return vault, nil
}
