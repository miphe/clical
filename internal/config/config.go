// Package config reads clical's settings file. Every setting is optional;
// a missing file means the defaults. Colors aren't settings: clical takes
// them from the terminal's palette.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"go.yaml.in/yaml/v3"
)

// Config is the settings clical runs with.
type Config struct {
	// Clock12 shows times as 3:04pm instead of 15:04.
	Clock12 bool
	// WeekStart is the first day of the week view and month grid.
	WeekStart time.Weekday
	// SyncInterval is how long after one sync finishes the next starts;
	// 0 turns background syncing off (r still syncs).
	SyncInterval time.Duration
	// Pulse makes an event pulse on screen shortly before it starts, and
	// PulseLead is how long before.
	Pulse     bool
	PulseLead time.Duration
	// PulseGrace is how long the pulse goes on after the event has started,
	// or GraceWholeEvent for as long as the event lasts.
	PulseGrace time.Duration
}

// GraceWholeEvent as a PulseGrace pulses until the event ends.
const GraceWholeEvent time.Duration = -1

// Bounds of PulseLead.
const (
	MinPulseLead  = time.Second
	MaxPulseLead  = time.Hour
	MaxPulseGrace = 24 * time.Hour
)

// MinSyncInterval keeps a feed provider from being polled too eagerly.
const MinSyncInterval = time.Minute

// Default is what clical runs with when nothing is set.
func Default() Config {
	return Config{WeekStart: time.Monday, SyncInterval: 5 * time.Minute, PulseLead: 5 * time.Minute, PulseGrace: time.Minute}
}

// DefaultPath is $XDG_CONFIG_HOME/clical/config.yaml
// (~/.config/clical/config.yaml).
func DefaultPath() string {
	return filepath.Join(xdg.ConfigHome, "clical", "config.yaml")
}

// Sample is a commented settings file with every setting at its default.
const Sample = `# clical settings. Every setting is optional; delete a line for its default.

# Clock: 24h (15:04) or 12h (3:04pm).
time_format: 24h

# First day of the week view and month grid: monday or sunday.
week_start: monday

# How long after one background sync finishes the next starts, e.g. 5m, 1h,
# or off. At least 1m.
sync_interval: 5m

# Pulse an event on screen shortly before it starts: true or false. Easier to
# change with S in the app, which saves to settings.yaml and overrides this.
pulse: false

# How long before the event starts, e.g. 10m, 5m, 1m or 30s.
pulse_lead: 5m

# How long the pulse goes on after the event has started: a duration like 5m,
# 0s for none, or event for as long as the event lasts.
pulse_grace: 1m
`

// PrefsPath is the file the settings dialog writes, beside config.yaml. The
// dialog never edits config.yaml, which is written by hand; settings made in
// the dialog override it.
func PrefsPath() string {
	return filepath.Join(xdg.ConfigHome, "clical", "settings.yaml")
}

// file is the settings file as written; empty strings are unset.
type file struct {
	TimeFormat   string `yaml:"time_format"`
	WeekStart    string `yaml:"week_start"`
	SyncInterval string `yaml:"sync_interval"`
	Pulse        *bool  `yaml:"pulse"`
	PulseLead    string `yaml:"pulse_lead"`
	PulseGrace   string `yaml:"pulse_grace"`
}

// Load reads the settings file at path. A missing file gives the defaults;
// an unreadable or invalid one is an error naming the file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read settings: %w", err)
	}
	c, err := parse(Default(), data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// LoadAll reads the hand-written file at path, then the dialog's file at
// prefs over it. An invalid hand-written file is an error; a damaged dialog
// file is ignored, so it can never keep clical from starting.
func LoadAll(path, prefs string) (Config, error) {
	c, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(prefs)
	if err != nil {
		return c, nil
	}
	if over, err := parse(c, data); err == nil {
		return over, nil
	}
	return c, nil
}

// Prefs are the settings the dialog owns.
type Prefs struct {
	Clock12      bool
	WeekStart    time.Weekday
	SyncInterval time.Duration
	Pulse        bool
	PulseLead    time.Duration
	PulseGrace   time.Duration
}

// SavePrefs writes p to path, atomically and readable only by the user.
func SavePrefs(path string, p Prefs) error {
	format := "24h"
	if p.Clock12 {
		format = "12h"
	}
	grace := "event"
	if p.PulseGrace != GraceWholeEvent {
		grace = DurationSetting(p.PulseGrace)
	}
	data := fmt.Sprintf("# Written by clical's settings dialog (S). Overrides config.yaml.\n"+
		"time_format: %s\nweek_start: %s\nsync_interval: %s\npulse: %t\npulse_lead: %s\npulse_grace: %s\n",
		format, strings.ToLower(p.WeekStart.String()), syncSetting(p.SyncInterval), p.Pulse,
		DurationSetting(p.PulseLead), grace)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DurationSetting writes d as the files do: 10m, 1h, 90s, 0s.
func DurationSetting(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%ds", d/time.Second)
}

func syncSetting(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	return DurationSetting(d)
}

// parse applies the settings in data over c.
func parse(c Config, data []byte) (Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a misspelt setting is an error, not ignored
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, err
	}

	switch strings.ToLower(strings.TrimSpace(f.TimeFormat)) {
	case "":
	case "24h":
		c.Clock12 = false
	case "12h":
		c.Clock12 = true
	default:
		return Config{}, fmt.Errorf("time_format must be 24h or 12h, not %q", f.TimeFormat)
	}
	switch strings.ToLower(strings.TrimSpace(f.WeekStart)) {
	case "":
	case "monday":
		c.WeekStart = time.Monday
	case "sunday":
		c.WeekStart = time.Sunday
	default:
		return Config{}, fmt.Errorf("week_start must be monday or sunday, not %q", f.WeekStart)
	}
	switch v := strings.ToLower(strings.TrimSpace(f.SyncInterval)); v {
	case "":
	case "off":
		c.SyncInterval = 0
	default:
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("sync_interval must be a duration like 5m or 1h, or off, not %q", f.SyncInterval)
		}
		if d < MinSyncInterval {
			return Config{}, fmt.Errorf("sync_interval must be at least %v, not %v", MinSyncInterval, d)
		}
		c.SyncInterval = d
	}
	if f.Pulse != nil {
		c.Pulse = *f.Pulse
	}
	if v := strings.TrimSpace(f.PulseLead); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < MinPulseLead || d > MaxPulseLead {
			return Config{}, fmt.Errorf("pulse_lead must be a duration from %v to %v like 5m or 30s, not %q", MinPulseLead, MaxPulseLead, f.PulseLead)
		}
		c.PulseLead = d
	}
	switch v := strings.ToLower(strings.TrimSpace(f.PulseGrace)); v {
	case "":
	case "event":
		c.PulseGrace = GraceWholeEvent
	default:
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 || d > MaxPulseGrace {
			return Config{}, fmt.Errorf("pulse_grace must be event, or a duration up to %v like 5m (0s for none), not %q", MaxPulseGrace, f.PulseGrace)
		}
		c.PulseGrace = d
	}
	return c, nil
}
