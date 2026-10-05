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
}

// MinSyncInterval keeps a feed provider from being polled too eagerly.
const MinSyncInterval = time.Minute

// Default is what clical runs with when nothing is set.
func Default() Config {
	return Config{WeekStart: time.Monday, SyncInterval: 5 * time.Minute}
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
`

// file is the settings file as written; empty strings are unset.
type file struct {
	TimeFormat   string `yaml:"time_format"`
	WeekStart    string `yaml:"week_start"`
	SyncInterval string `yaml:"sync_interval"`
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
	c, err := parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

func parse(data []byte) (Config, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a misspelt setting is an error, not ignored
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, err
	}

	c := Default()
	switch strings.ToLower(strings.TrimSpace(f.TimeFormat)) {
	case "", "24h":
	case "12h":
		c.Clock12 = true
	default:
		return Config{}, fmt.Errorf("time_format must be 24h or 12h, not %q", f.TimeFormat)
	}
	switch strings.ToLower(strings.TrimSpace(f.WeekStart)) {
	case "", "monday":
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
	return c, nil
}
