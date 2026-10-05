package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogRecordsChangesNotSecrets(t *testing.T) {
	dir := t.TempDir()
	p := Paths{Vault: filepath.Join(dir, "vault"), DB: filepath.Join(dir, "clical.db"), Log: filepath.Join(dir, "state", "clical.log")}
	a, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	cal, err := a.AddCalendar(ctx, "Personal", "Main", feed("https://x.test/very-secret-token.ics"))
	if err != nil {
		t.Fatal(err)
	}
	a.outcome(SyncOutcome{CalendarInfo: cal, Err: errors.New("HTTP 503")})
	a.outcome(SyncOutcome{CalendarInfo: cal})                // recovered
	a.outcome(SyncOutcome{CalendarInfo: cal})                // quiet: not logged
	a.outcome(SyncOutcome{CalendarInfo: cal, Changed: true}) // changes
	if err := a.SetCalendarURL(ctx, cal, feed("https://x.test/another-secret.ics")); err != nil {
		t.Fatal(err)
	}
	a.Close()

	b, err := os.ReadFile(p.Log)
	if err != nil {
		t.Fatal(err)
	}
	log := string(b)
	for _, want := range []string{
		`msg="calendar added" calendar="Personal · Main"`,
		`msg="sync failed" calendar="Personal · Main" error="HTTP 503"`,
		`msg="sync works again"`,
		`msg="synced changes"`,
		`msg="calendar address replaced"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %s:\n%s", want, log)
		}
	}
	if n := strings.Count(log, "\n"); n != 5 {
		t.Errorf("log has %d lines, want 5 (quiet syncs aren't logged):\n%s", n, log)
	}
	if strings.Contains(log, "secret") || strings.Contains(log, "x.test") {
		t.Errorf("a feed address is in the log:\n%s", log)
	}
	if fi, err := os.Stat(p.Log); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("log file mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
}

func TestLogIsRotatedWhenLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clical.log")
	if err := os.WriteFile(path, make([]byte, maxLogSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	log, f, err := openLog(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("fresh")
	f.Close()
	if fi, err := os.Stat(path + ".1"); err != nil || fi.Size() != maxLogSize+1 {
		t.Errorf("old log not kept as .1: %v", err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "fresh") || len(b) > 200 {
		t.Errorf("new log = %q", b)
	}
}
