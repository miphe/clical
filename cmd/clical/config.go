package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/miphe/clical/internal/agenda"
	"github.com/miphe/clical/internal/config"
)

// ConfigCmd shows or creates the settings file.
type ConfigCmd struct {
	Show ConfigShowCmd `cmd:"" default:"1" help:"Print where the settings file is and the settings in use."`
	Init ConfigInitCmd `cmd:"" help:"Write a settings file with every setting at its default."`
}

// loadConfig reads the settings file; an invalid one stops clical with an
// error naming the file and the setting.
func loadConfig() (config.Config, error) {
	return config.Load(config.DefaultPath())
}

func clockFor(c config.Config) agenda.Clock { return agenda.Clock{Twelve: c.Clock12} }

// ConfigShowCmd prints the settings file's path and the settings in use.
type ConfigShowCmd struct{}

func (c *ConfigShowCmd) Run() error {
	path := config.DefaultPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fmt.Printf("%s (not created; using defaults — 'clical config init' writes one)\n\n", path)
	} else {
		fmt.Printf("%s\n\n", path)
	}
	timeFormat := "24h"
	if cfg.Clock12 {
		timeFormat = "12h"
	}
	fmt.Printf("time_format:   %s\nweek_start:    %s\nsync_interval: %s\n",
		timeFormat, strings.ToLower(cfg.WeekStart.String()), intervalSetting(cfg.SyncInterval))
	return nil
}

// intervalSetting writes d as the settings file would: 5m, 1h30m, off.
func intervalSetting(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	s := d.String() // e.g. 1h30m0s
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// ConfigInitCmd writes the sample settings file, never over an existing one.
type ConfigInitCmd struct{}

func (c *ConfigInitCmd) Run() error {
	path := config.DefaultPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists; edit it instead", path)
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(config.Sample); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println("Wrote", path)
	return nil
}
