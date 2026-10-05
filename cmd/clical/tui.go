package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/config"
	"github.com/miphe/clical/internal/ui"
)

// TUICmd opens the full-screen calendar. It's the default command.
type TUICmd struct{}

func (c *TUICmd) Run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
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
	m := ui.New(a, time.Local, time.Now()).WithWrite(a).WithSettings(ui.Settings{
		Clock12:      cfg.Clock12,
		WeekStart:    cfg.WeekStart,
		SyncInterval: cfg.SyncInterval,
		Pulse:        cfg.Pulse,
		PulseLead:    cfg.PulseLead,
		PulseGrace:   cfg.PulseGrace,
		PanelFlash:   cfg.PanelFlash,
		Save: func(p ui.Prefs) error {
			return config.SavePrefs(config.PrefsPath(), config.Prefs(p))
		},
	})
	_, err = tea.NewProgram(m).Run()
	return err
}
