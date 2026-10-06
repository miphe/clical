package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/miphe/clical/internal/config"
	"github.com/miphe/clical/internal/mock"
	"github.com/miphe/clical/internal/ui"
)

// TUICmd opens the full-screen calendar. It's the default command.
type TUICmd struct{}

func (c *TUICmd) Run(cli *CLI) error {
	if cli.Mock { // --mock, see runMock
		return runMock()
	}
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
		Write:        cfg.Write,
		Save: func(p ui.Prefs) error {
			return config.SavePrefs(config.PrefsPath(), config.Prefs(p))
		},
	})
	_, err = tea.NewProgram(m).Run()
	return err
}

// runMock opens the TUI on generated data, in a temporary directory that is
// removed on exit. It never reads the settings, vault, cache, notes or log, and
// settings changed in it are not saved.
func runMock() error {
	f, err := mock.Open(time.Now(), time.Local)
	if err != nil {
		return err
	}
	defer f.Close()
	var src ui.Source = f.Source
	m := ui.New(src, time.Local, time.Now()).WithWrite(f.App).WithSettings(ui.Settings{
		WeekStart:    time.Monday,
		SyncInterval: 5 * time.Minute,
		Pulse:        true,
		PulseLead:    10 * time.Minute,
		PulseGrace:   time.Minute,
		PanelFlash:   true,
		Write:        true,
		Save:         func(ui.Prefs) error { return nil },
	}).WithNotice(mock.Notice)
	_, err = tea.NewProgram(m).Run()
	return err
}
