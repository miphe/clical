package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/miphe/clical/internal/app"
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

const noCalendarsHint = "No calendars yet. Add one with `clical calendars add --account <group> --name <name>`."

func openApp() (*app.App, error) {
	paths := app.DefaultPaths()
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.WriteDir != "" {
		paths.Write = cfg.WriteDir
	}
	return app.Open(paths)
}

// unlock prompts for the vault passphrase and unlocks a. If no vault exists
// yet, the user chooses a new passphrase and must type it twice.
func unlock(a *app.App) error {
	exists, err := a.VaultExists()
	if err != nil {
		return err
	}
	var passphrase string
	if exists {
		passphrase, err = promptHidden("Vault passphrase: ")
	} else {
		fmt.Fprintln(os.Stderr, "Creating a new vault. Choose a passphrase — it can't be recovered if forgotten.")
		passphrase, err = choosePassphrase(promptHidden)
	}
	if err != nil {
		return err
	}
	if err := a.Unlock(passphrase); err != nil {
		return fmt.Errorf("open vault: %w", err)
	}
	return nil
}

// choosePassphrase asks for a new passphrase twice and requires both to
// match and be non-empty.
func choosePassphrase(prompt func(string) (string, error)) (string, error) {
	first, err := prompt("New vault passphrase: ")
	if err != nil {
		return "", err
	}
	if first == "" {
		return "", errors.New("passphrase must not be empty")
	}
	second, err := prompt("Confirm passphrase: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passphrases don't match")
	}
	return first, nil
}

// confirm asks a yes/no question on the terminal; anything but y/yes is no.
func confirm(question string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, errors.New("a terminal is required to confirm; pass --yes to skip")
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false, fmt.Errorf("read answer: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
