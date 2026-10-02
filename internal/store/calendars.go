package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/miphe/clical/internal/domain"
)

// UpsertCalendar inserts cal, or updates its name and color slot in place if
// it already exists. The calendar's sync token is left untouched on update.
func (s *Store) UpsertCalendar(ctx context.Context, cal domain.Calendar) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO calendars (id, account_id, name, color) VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, id) DO UPDATE SET name = excluded.name, color = excluded.color
	`, cal.ID, cal.AccountID, cal.Name, encodeSlot(cal.ColorSlot))
	if err != nil {
		return fmt.Errorf("store: upsert calendar: %w", err)
	}
	return nil
}

// ListCalendars returns every known calendar, ordered by account then name.
func (s *Store) ListCalendars(ctx context.Context) ([]domain.Calendar, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.account_id, c.name, c.color
		FROM calendars c JOIN accounts a ON a.id = c.account_id
		ORDER BY a.label, c.name
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list calendars: %w", err)
	}
	defer rows.Close()

	var out []domain.Calendar
	for rows.Next() {
		var c domain.Calendar
		var slot string
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Name, &slot); err != nil {
			return nil, fmt.Errorf("store: scan calendar: %w", err)
		}
		c.ColorSlot = decodeSlot(slot)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list calendars: %w", err)
	}
	return out, nil
}

// The color column predates color slots and holds the slot as decimal text;
// ” (from before slots existed) reads as 0, unassigned.
func encodeSlot(slot int) string {
	if slot <= 0 {
		return ""
	}
	return strconv.Itoa(slot)
}

func decodeSlot(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// DeleteCalendar removes cal and its events, and removes its account too if
// that leaves the account with no calendars.
func (s *Store) DeleteCalendar(ctx context.Context, cal domain.Calendar) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM events WHERE account_id = ? AND calendar_id = ?`, cal.AccountID, cal.ID,
		); err != nil {
			return fmt.Errorf("store: delete events: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM calendars WHERE account_id = ? AND id = ?`, cal.AccountID, cal.ID,
		); err != nil {
			return fmt.Errorf("store: delete calendar: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM accounts WHERE id = ? AND NOT EXISTS (SELECT 1 FROM calendars WHERE account_id = ?)`,
			cal.AccountID, cal.AccountID,
		); err != nil {
			return fmt.Errorf("store: delete empty account: %w", err)
		}
		return nil
	})
}

// ResetSyncToken clears cal's sync token so the next sync is a full one.
func (s *Store) ResetSyncToken(ctx context.Context, cal domain.Calendar) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if _, err := s.db.ExecContext(ctx,
		`UPDATE calendars SET sync_token = NULL WHERE account_id = ? AND id = ?`, cal.AccountID, cal.ID,
	); err != nil {
		return fmt.Errorf("store: reset sync token: %w", err)
	}
	return nil
}
