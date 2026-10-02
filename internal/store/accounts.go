package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/miphe/clical/internal/domain"
)

// ErrNotFound is returned when a lookup by ID finds nothing.
var ErrNotFound = errors.New("store: not found")

// UpsertAccount inserts acct, or updates it in place if its ID already exists.
// The account's nickname is left untouched (see SetAccountNickname).
func (s *Store) UpsertAccount(ctx context.Context, acct domain.Account) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO accounts (id, provider, label) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET provider = excluded.provider, label = excluded.label
	`, acct.ID, acct.Provider, acct.Label)
	if err != nil {
		return fmt.Errorf("store: upsert account: %w", err)
	}
	return nil
}

// ListAccounts returns every known account, ordered by label.
func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider, label, nickname FROM accounts
		ORDER BY COALESCE(NULLIF(nickname, ''), label) COLLATE NOCASE
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	defer rows.Close()

	var out []domain.Account
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.ID, &a.Provider, &a.Label, &a.Nickname); err != nil {
			return nil, fmt.Errorf("store: scan account: %w", err)
		}
		a.Label, a.Nickname = domain.SafeText(a.Label), domain.SafeText(a.Nickname)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list accounts: %w", err)
	}
	return out, nil
}

// GetAccount looks up a single account by ID, returning ErrNotFound if it
// doesn't exist.
func (s *Store) GetAccount(ctx context.Context, id string) (domain.Account, error) {
	var a domain.Account
	err := s.db.QueryRowContext(ctx, `SELECT id, provider, label, nickname FROM accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.Provider, &a.Label, &a.Nickname)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("store: get account: %w", err)
	}
	a.Label, a.Nickname = domain.SafeText(a.Label), domain.SafeText(a.Nickname)
	return a, nil
}

// SetAccountNickname sets the user's own name for an account; "" removes it.
func (s *Store) SetAccountNickname(ctx context.Context, id, nickname string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET nickname = ? WHERE id = ?`, nickname, id)
	if err != nil {
		return fmt.Errorf("store: set account nickname: %w", err)
	}
	return requireRow(res)
}

// requireRow returns ErrNotFound if res changed no rows.
func requireRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
