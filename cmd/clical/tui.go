package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/ui"
)

// TUICmd opens the full-screen calendar. It's the default command.
type TUICmd struct{}

func (c *TUICmd) Run() error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()

	// Unlocked once, before the TUI takes over the terminal; the passphrase
	// then lives in memory for the session. With no calendars yet the TUI
	// still opens, so they can be added from inside it.
	if err := unlock(a); err != nil {
		return err
	}
	_, err = tea.NewProgram(ui.New(a, time.Local, time.Now())).Run()
	return err
}
