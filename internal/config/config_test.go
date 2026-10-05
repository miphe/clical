package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil || c != Default() {
		t.Errorf("Load(missing) = %+v, %v", c, err)
	}
}

func TestSampleIsTheDefaults(t *testing.T) {
	c, err := parse([]byte(Sample))
	if err != nil || c != Default() {
		t.Errorf("parse(Sample) = %+v, %v; want the defaults", c, err)
	}
}

func TestSettings(t *testing.T) {
	c, err := parse([]byte("time_format: 12H\nweek_start: Sunday\nsync_interval: 15m\n"))
	want := Config{Clock12: true, WeekStart: time.Sunday, SyncInterval: 15 * time.Minute}
	if err != nil || c != want {
		t.Errorf("parse() = %+v, %v; want %+v", c, err, want)
	}
	if c, err := parse([]byte("sync_interval: off\n")); err != nil || c.SyncInterval != 0 {
		t.Errorf("sync_interval off = %+v, %v", c, err)
	}
	if c, err := parse([]byte("# only comments\n")); err != nil || c != Default() {
		t.Errorf("comments only = %+v, %v", c, err)
	}
}

func TestInvalidSettingsAreErrors(t *testing.T) {
	for _, in := range []string{
		"time_format: 13h",
		"week_start: friday",
		"sync_interval: soon",
		"sync_interval: 10s",
		"week_starts: sunday", // misspelt
		"time_format: [24h",
	} {
		if _, err := parse([]byte(in)); err == nil {
			t.Errorf("parse(%q) error = nil", in)
		}
	}
}

func TestLoadNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("week_start: friday\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("Load() error = %v, want it to name %s", err, path)
	}
}
