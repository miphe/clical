package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/miphe/clical/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "clical.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndListAccounts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	a1 := domain.Account{ID: "a1", Provider: "google", Label: "b@example.com"}
	a2 := domain.Account{ID: "a2", Provider: "google", Label: "a@example.com"}
	if err := s.UpsertAccount(ctx, a1); err != nil {
		t.Fatalf("UpsertAccount(a1) error = %v", err)
	}
	if err := s.UpsertAccount(ctx, a2); err != nil {
		t.Fatalf("UpsertAccount(a2) error = %v", err)
	}

	got, err := s.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAccounts() returned %d accounts, want 2", len(got))
	}
	// Ordered by label: a@example.com before b@example.com.
	if got[0].ID != "a2" || got[1].ID != "a1" {
		t.Errorf("ListAccounts() order = [%s, %s], want [a2, a1]", got[0].ID, got[1].ID)
	}
}

func TestUpsertAccountUpdatesInPlace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	acct := domain.Account{ID: "a1", Provider: "google", Label: "old@example.com"}
	if err := s.UpsertAccount(ctx, acct); err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}

	acct.Label = "new@example.com"
	if err := s.UpsertAccount(ctx, acct); err != nil {
		t.Fatalf("UpsertAccount() update error = %v", err)
	}

	all, err := s.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListAccounts() returned %d accounts, want 1", len(all))
	}
	if all[0].Label != "new@example.com" {
		t.Errorf("Label = %q, want %q", all[0].Label, "new@example.com")
	}
}

func TestGetAccountNotFound(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetAccount(context.Background(), "missing"); err != ErrNotFound {
		t.Errorf("GetAccount() error = %v, want %v", err, ErrNotFound)
	}
}
