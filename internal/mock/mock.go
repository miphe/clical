// Package mock is a made-up world for developing and trying clical without
// touching anything real: calendars, events, notes, TODOs and a year and a half
// of completed tasks, all generated from the clock and a fixed seed. Nothing
// is read from or written to the user's data, config, vault or log, and the
// network is never used. It is meant for `clical --mock`, where a screen that
// is barren with real data (the heatmap, a crowded agenda, a failing
// calendar) can be looked at and refined.
//
// The notes live in a real notes store in a temporary directory, so the Write
// panels, the archive and the heatmap run the same code as with real data; the
// calendars and events are served from memory.
package mock

import (
	"os"
	"path/filepath"
	"time"

	"github.com/miphe/clical/internal/app"
)

// Notice is what the TUI shows so mock data is never mistaken for real data.
const Notice = "MOCK DATA"

// Fixture is one run's mock world.
type Fixture struct {
	Source *Source  // calendars and events, for the TUI
	App    *app.App // the notes, which the TUI reaches as its WriteSource
	dir    string
}

// Open builds the world around now, in loc. Close removes it.
func Open(now time.Time, loc *time.Location) (*Fixture, error) {
	dir, err := os.MkdirTemp("", "clical-mock-")
	if err != nil {
		return nil, err
	}
	a, err := app.Open(app.Paths{
		Vault: filepath.Join(dir, "vault"),
		DB:    filepath.Join(dir, "clical.db"),
		Write: filepath.Join(dir, "write"),
	})
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	f := &Fixture{App: a, dir: dir}
	events := newEvents(now, loc)
	f.Source = newSource(now, loc, events)
	if err := seedWrite(a, now, loc, events); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// Close removes the temporary directory.
func (f *Fixture) Close() error {
	f.App.Close()
	return os.RemoveAll(f.dir)
}
