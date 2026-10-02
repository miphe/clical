package main

import (
	"context"
	"fmt"
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

	cals, err := a.Calendars(context.Background())
	if err != nil {
		return err
	}
	if len(cals) == 0 {
		fmt.Println(noCalendarsHint)
		return nil
	}
	// Unlocked once, before the TUI takes over the terminal; the passphrase
	// then lives in memory for the session.
	if err := unlock(a); err != nil {
		return err
	}
	_, err = tea.NewProgram(ui.New(a, time.Local, time.Now())).Run()
	return err
}
