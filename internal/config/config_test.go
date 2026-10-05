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
	c, err := parse(Default(), []byte(Sample))
	if err != nil || c != Default() {
		t.Errorf("parse(Sample) = %+v, %v; want the defaults", c, err)
	}
}

func TestSettings(t *testing.T) {
	c, err := parse(Default(), []byte("time_format: 12H\nweek_start: Sunday\nsync_interval: 15m\n"))
	want := Config{Clock12: true, WeekStart: time.Sunday, SyncInterval: 15 * time.Minute, PulseLead: 5 * time.Minute, PulseGrace: time.Minute}
	if err != nil || c != want {
		t.Errorf("parse() = %+v, %v; want %+v", c, err, want)
	}
	if c, err := parse(Default(), []byte("sync_interval: off\n")); err != nil || c.SyncInterval != 0 {
		t.Errorf("sync_interval off = %+v, %v", c, err)
	}
	if c, err := parse(Default(), []byte("# only comments\n")); err != nil || c != Default() {
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
		if _, err := parse(Default(), []byte(in)); err == nil {
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

func TestPulseSettings(t *testing.T) {
	c, err := parse(Default(), []byte("pulse: true\npulse_lead: 90s\n"))
	if err != nil || !c.Pulse || c.PulseLead != 90*time.Second {
		t.Errorf("parse() = %+v, %v", c, err)
	}
	g, err := parse(Default(), []byte("pulse_grace: event\n"))
	if err != nil || g.PulseGrace != GraceWholeEvent {
		t.Errorf("pulse_grace: event = %+v, %v", g, err)
	}
	if g, err := parse(Default(), []byte("pulse_grace: 0s\n")); err != nil || g.PulseGrace != 0 {
		t.Errorf("pulse_grace: 0s = %+v, %v", g, err)
	}
	for _, bad := range []string{"pulse_grace: -1m", "pulse_grace: 48h", "pulse_grace: later", "pulse_lead: 0s", "pulse_lead: 2h", "pulse_lead: soon", "pulse: maybe"} {
		if _, err := parse(Default(), []byte(bad+"\n")); err == nil {
			t.Errorf("parse(%q) succeeded", bad)
		}
	}
}

func TestPrefsOverrideTheFileAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, prefs := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte("time_format: 12h\nweek_start: sunday\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := Prefs{Clock12: true, WeekStart: time.Monday, SyncInterval: 45 * time.Minute, Pulse: true, PulseLead: 90 * time.Second, PulseGrace: GraceWholeEvent}
	if err := SavePrefs(prefs, want); err != nil {
		t.Fatal(err)
	}
	c, err := LoadAll(path, prefs)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Clock12 || !c.Pulse || c.PulseLead != 90*time.Second || c.WeekStart != time.Monday ||
		c.SyncInterval != 45*time.Minute || c.PulseGrace != GraceWholeEvent {
		t.Errorf("LoadAll = %+v; the dialog's file should win over config.yaml", c)
	}
	off := want
	off.SyncInterval, off.PulseGrace = 0, 7*time.Minute
	if err := SavePrefs(prefs, off); err != nil {
		t.Fatal(err)
	}
	if c, _ := LoadAll(path, prefs); c.SyncInterval != 0 || c.PulseGrace != 7*time.Minute {
		t.Errorf("LoadAll = %+v; want sync off and a 7m grace", c)
	}
	if fi, _ := os.Stat(prefs); fi.Mode().Perm() != 0o600 {
		t.Errorf("settings.yaml mode = %v", fi.Mode().Perm())
	}
}

func TestDamagedPrefsAreIgnored(t *testing.T) {
	dir := t.TempDir()
	prefs := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(prefs, []byte("pulse: [oops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadAll(filepath.Join(dir, "none.yaml"), prefs)
	if err != nil || c != Default() {
		t.Errorf("LoadAll = %+v, %v; want the defaults", c, err)
	}
}
