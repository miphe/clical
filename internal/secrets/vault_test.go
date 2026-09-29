package secrets

import (
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func fixedPrompt(passphrase string) func() (string, error) {
	return func() (string, error) { return passphrase, nil }
}

func TestSaveLoadTokenRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v, err := Open(dir, fixedPrompt("correct-horse-battery-staple"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	want := &oauth2.Token{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
	if err := v.SaveToken("acct1", want); err != nil {
		t.Fatalf("SaveToken() error = %v", err)
	}

	got, err := v.LoadToken("acct1")
	if err != nil {
		t.Fatalf("LoadToken() error = %v", err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || !got.Expiry.Equal(want.Expiry) {
		t.Errorf("LoadToken() = %+v, want %+v", got, want)
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v, err := Open(dir, fixedPrompt("right-passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := v.SaveToken("acct1", &oauth2.Token{AccessToken: "x"}); err != nil {
		t.Fatalf("SaveToken() error = %v", err)
	}

	if _, err := Open(dir, fixedPrompt("wrong-passphrase")); err != ErrWrongPassphrase {
		t.Errorf("Open() with wrong passphrase error = %v, want %v", err, ErrWrongPassphrase)
	}

	// Right passphrase still works after the failed attempt.
	if _, err := Open(dir, fixedPrompt("right-passphrase")); err != nil {
		t.Errorf("Open() with correct passphrase error = %v, want nil", err)
	}
}

func TestDeleteToken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v, err := Open(dir, fixedPrompt("passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := v.SaveToken("acct1", &oauth2.Token{AccessToken: "x"}); err != nil {
		t.Fatalf("SaveToken() error = %v", err)
	}
	if err := v.DeleteToken("acct1"); err != nil {
		t.Fatalf("DeleteToken() error = %v", err)
	}
	if _, err := v.LoadToken("acct1"); err == nil {
		t.Error("LoadToken() after delete: got nil error, want not found")
	}
	// Deleting an already-deleted token is not an error.
	if err := v.DeleteToken("acct1"); err != nil {
		t.Errorf("DeleteToken() on missing token error = %v, want nil", err)
	}
}

func TestOpenEmptyVaultAcceptsAnyPassphrase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	if _, err := Open(dir, fixedPrompt("anything")); err != nil {
		t.Errorf("Open() on empty vault error = %v, want nil", err)
	}
}

func TestSaveLoadSecretRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v, err := Open(dir, fixedPrompt("passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	want := "https://calendar.google.com/calendar/ical/x/private-abc/basic.ics"
	if err := v.SaveSecret("ical-url-abc123", []byte(want)); err != nil {
		t.Fatalf("SaveSecret() error = %v", err)
	}
	got, err := v.LoadSecret("ical-url-abc123")
	if err != nil {
		t.Fatalf("LoadSecret() error = %v", err)
	}
	if string(got) != want {
		t.Errorf("LoadSecret() = %q, want %q", got, want)
	}
}

func TestSecretKeyRejectsPathTraversal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	v, err := Open(dir, fixedPrompt("passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, key := range []string{"../escape", "a/b", "", "a.b"} {
		if err := v.SaveSecret(key, []byte("x")); err == nil {
			t.Errorf("SaveSecret(%q) error = nil, want invalid key", key)
		}
	}
}
