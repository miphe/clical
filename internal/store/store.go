// Package store persists accounts, calendars, events, and sync tokens in a
// local SQLite cache (pure-Go driver, no cgo, for painless cross-compilation).
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Store is a handle on the local cache database. All writes are serialized
// through writeMu: the pure-Go sqlite driver has no tolerance for concurrent
// writers even with WAL mode enabled.
type Store struct {
	db      *sql.DB
	writeMu sync.Mutex
}

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
	id       TEXT PRIMARY KEY,
	provider TEXT NOT NULL,
	label    TEXT NOT NULL,
	nickname TEXT NOT NULL DEFAULT '' -- the user's own name; '' = none
);

CREATE TABLE IF NOT EXISTS calendars (
	id             TEXT NOT NULL,
	account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	name           TEXT NOT NULL,
	nickname       TEXT NOT NULL DEFAULT '', -- the user's own name; '' = none
	color          TEXT NOT NULL, -- color slot as decimal text; '' = unassigned
	sync_token     TEXT,
	last_synced_at INTEGER,
	PRIMARY KEY (account_id, id)
);

CREATE TABLE IF NOT EXISTS events (
	id          TEXT NOT NULL,
	calendar_id TEXT NOT NULL,
	account_id  TEXT NOT NULL,
	title       TEXT NOT NULL,
	all_day     INTEGER NOT NULL,
	start_utc   INTEGER,
	end_utc     INTEGER,
	start_date  TEXT,
	end_date    TEXT,
	location    TEXT NOT NULL DEFAULT '',
	notes       TEXT NOT NULL DEFAULT '',
	links       TEXT NOT NULL DEFAULT '', -- JSON array of links; '' = none
	key         TEXT NOT NULL DEFAULT '', -- stable identity, see domain.Event.Key
	PRIMARY KEY (account_id, calendar_id, id),
	FOREIGN KEY (account_id, calendar_id) REFERENCES calendars(account_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_events_range       ON events (calendar_id, start_utc, end_utc);
CREATE INDEX IF NOT EXISTS idx_events_alldayrange ON events (calendar_id, start_date, end_date);
`

// Open opens (creating if necessary) the SQLite database at path and applies
// the schema.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	// Columns added after the first release: CREATE TABLE IF NOT EXISTS
	// doesn't add them to an existing database.
	added := []struct{ table, column, def string }{
		{"events", "links", "TEXT NOT NULL DEFAULT ''"},
		{"events", "key", "TEXT NOT NULL DEFAULT ''"},
		{"accounts", "nickname", "TEXT NOT NULL DEFAULT ''"},
		{"calendars", "nickname", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range added {
		exists, err := s.hasColumn(ctx, c.table, c.column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE "+c.table+" ADD COLUMN "+c.column+" "+c.def); err != nil {
				return fmt.Errorf("store: add %s.%s: %w", c.table, c.column, err)
			}
		}
	}
	return nil
}

func (s *Store) hasColumn(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, fmt.Errorf("store: inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit tx: %w", err)
	}
	return nil
}
