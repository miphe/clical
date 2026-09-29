package store

import (
	"context"
	"fmt"

	"github.com/miphe/clical/internal/domain"
)

// UpsertCalendar inserts cal, or updates its name and color in place if it
// already exists. The calendar's sync token is left untouched on update.
func (s *Store) UpsertCalendar(ctx context.Context, cal domain.Calendar) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO calendars (id, account_id, name, color) VALUES (?, ?, ?, ?)
		ON CONFLICT(account_id, id) DO UPDATE SET name = excluded.name, color = excluded.color
	`, cal.ID, cal.AccountID, cal.Name, cal.Color)
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
		if err := rows.Scan(&c.ID, &c.AccountID, &c.Name, &c.Color); err != nil {
			return nil, fmt.Errorf("store: scan calendar: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list calendars: %w", err)
	}
	return out, nil
}
