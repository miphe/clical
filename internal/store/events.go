package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/miphe/clical/internal/domain"
)

// SyncToken returns the sync token persisted for cal, or "" if it has never
// been synced.
func (s *Store) SyncToken(ctx context.Context, cal domain.Calendar) (string, error) {
	var tok sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT sync_token FROM calendars WHERE account_id = ? AND id = ?`, cal.AccountID, cal.ID,
	).Scan(&tok)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: sync token: %w", err)
	}
	return tok.String, nil
}

// ApplySync applies one provider sync result for cal in a single
// transaction, so the events and the new sync token land together or not at
// all. A full resync replaces all of cal's events.
func (s *Store) ApplySync(ctx context.Context, cal domain.Calendar, r domain.SyncResult) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return s.withTx(ctx, func(tx *sql.Tx) error {
		if r.FullResync {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM events WHERE account_id = ? AND calendar_id = ?`, cal.AccountID, cal.ID,
			); err != nil {
				return fmt.Errorf("store: clear events: %w", err)
			}
		} else {
			for _, id := range r.DeletedIDs {
				if _, err := tx.ExecContext(ctx,
					`DELETE FROM events WHERE account_id = ? AND calendar_id = ? AND id = ?`, cal.AccountID, cal.ID, id,
				); err != nil {
					return fmt.Errorf("store: delete event: %w", err)
				}
			}
		}

		if len(r.Upserted) > 0 {
			stmt, err := tx.PrepareContext(ctx, `
				INSERT OR REPLACE INTO events
					(id, calendar_id, account_id, title, all_day, start_utc, end_utc, start_date, end_date, location, notes, links)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`)
			if err != nil {
				return fmt.Errorf("store: prepare upsert: %w", err)
			}
			defer stmt.Close()

			for _, e := range r.Upserted {
				var startUTC, endUTC, startDate, endDate any
				if ds, de, ok := e.DateRange(); ok {
					startDate, endDate = ds.String(), de.String()
				} else {
					ts, te, _ := e.TimeRange()
					startUTC, endUTC = ts.Unix(), te.Unix()
				}
				links, err := encodeLinks(e.Links)
				if err != nil {
					return err
				}
				if _, err := stmt.ExecContext(ctx,
					e.ID, cal.ID, cal.AccountID, e.Title, e.AllDay, startUTC, endUTC, startDate, endDate, e.Location, e.Notes, links,
				); err != nil {
					return fmt.Errorf("store: upsert event: %w", err)
				}
			}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE calendars SET sync_token = ?, last_synced_at = ? WHERE account_id = ? AND id = ?`,
			r.NextToken, time.Now().Unix(), cal.AccountID, cal.ID,
		); err != nil {
			return fmt.Errorf("store: update sync token: %w", err)
		}
		return nil
	})
}

// EventsInRange returns every event overlapping the local-time window
// [from, to): timed events whose [start, end) overlaps it (zero-length events
// count if they start inside it), and all-day events overlapping the civil
// dates the window covers.
func (s *Store) EventsInRange(ctx context.Context, from, to time.Time) ([]domain.Event, error) {
	fromDate := domain.CivilDateFromTime(from).String()
	// to is exclusive; the last civil date touched is the one just before it.
	toDate := domain.CivilDateFromTime(to.Add(-time.Nanosecond)).AddDays(1).String()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, calendar_id, title, all_day, start_utc, end_utc, start_date, end_date, location, notes, links
		FROM events
		WHERE (all_day = 0 AND start_utc < ? AND max(end_utc, start_utc + 1) > ?)
		   OR (all_day = 1 AND start_date < ? AND end_date > ?)
		ORDER BY coalesce(start_date, ''), coalesce(start_utc, 0), title
	`, to.Unix(), from.Unix(), toDate, fromDate)
	if err != nil {
		return nil, fmt.Errorf("store: events in range: %w", err)
	}
	defer rows.Close()

	var out []domain.Event
	for rows.Next() {
		var (
			id, calID, title, location, notes, linksJSON string
			allDay                                       bool
			startUTC, endUTC                             sql.NullInt64
			startDate, endDate                           sql.NullString
		)
		if err := rows.Scan(&id, &calID, &title, &allDay, &startUTC, &endUTC, &startDate, &endDate, &location, &notes, &linksJSON); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		links, err := decodeLinks(linksJSON)
		if err != nil {
			return nil, fmt.Errorf("store: event %s: %w", id, err)
		}
		if allDay {
			sd, err := domain.ParseCivilDate(startDate.String)
			if err != nil {
				return nil, fmt.Errorf("store: event %s: %w", id, err)
			}
			ed, err := domain.ParseCivilDate(endDate.String)
			if err != nil {
				return nil, fmt.Errorf("store: event %s: %w", id, err)
			}
			out = append(out, domain.NewAllDayEvent(id, calID, title, sd, ed, location, notes).WithLinks(links))
			continue
		}
		out = append(out, domain.NewTimedEvent(id, calID, title,
			time.Unix(startUTC.Int64, 0), time.Unix(endUTC.Int64, 0), location, notes).WithLinks(links))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: events in range: %w", err)
	}
	return out, nil
}

// storedLink is a link's stored form; kept separate from domain.Link so the
// JSON field names stay stable.
type storedLink struct {
	URL   string          `json:"url"`
	Label string          `json:"label,omitempty"`
	Kind  domain.LinkKind `json:"kind,omitempty"`
}

func encodeLinks(links []domain.Link) (string, error) {
	if len(links) == 0 {
		return "", nil
	}
	stored := make([]storedLink, len(links))
	for i, l := range links {
		stored[i] = storedLink{URL: l.URL, Label: l.Label, Kind: l.Kind}
	}
	b, err := json.Marshal(stored)
	if err != nil {
		return "", fmt.Errorf("store: encode links: %w", err)
	}
	return string(b), nil
}

func decodeLinks(s string) ([]domain.Link, error) {
	if s == "" {
		return nil, nil
	}
	var stored []storedLink
	if err := json.Unmarshal([]byte(s), &stored); err != nil {
		return nil, fmt.Errorf("decode links: %w", err)
	}
	links := make([]domain.Link, len(stored))
	for i, l := range stored {
		links[i] = domain.Link{URL: l.URL, Label: l.Label, Kind: l.Kind}
	}
	return links, nil
}
