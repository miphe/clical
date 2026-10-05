package app

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// clical keeps a small log of what happened in the background, so a failed
// sync can be looked into after the status line has moved on: sync
// failures and recoveries, calendars changing, and calendar and account
// changes. Only names go in; feed addresses and tokens never do (provider
// errors are already redacted, see the providers' tests).

// maxLogSize is when the log is rotated (on opening): the old one is kept as
// clical.log.1, replacing the one before.
const maxLogSize = 1 << 20

func openLog(path string) (*slog.Logger, io.Closer, error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), io.NopCloser(nil), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, nil, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return slog.New(slog.NewTextHandler(f, nil)), f, nil
}

// logOutcome records a sync's result when there's something to say: a
// failure, a recovery, or changed events. Quiet syncs aren't logged.
func (a *App) logOutcome(o SyncOutcome, recovered bool) {
	switch {
	case o.Err != nil:
		a.log.Warn("sync failed", "calendar", o.Label(), "error", o.Err.Error())
	case recovered:
		a.log.Info("sync works again", "calendar", o.Label())
	case o.Changed:
		a.log.Info("synced changes", "calendar", o.Label())
	}
}
