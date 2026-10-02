package secrets

import (
	"os"
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

func TestExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")

	if ok, err := Exists(dir); err != nil || ok {
		t.Errorf("Exists(missing dir) = %v, %v; want false, nil", ok, err)
	}

	v, err := Open(dir, fixedPrompt("passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if ok, err := Exists(dir); err != nil || ok {
		t.Errorf("Exists(empty vault) = %v, %v; want false, nil", ok, err)
	}

	if err := v.SaveSecret("k", []byte("x")); err != nil {
		t.Fatalf("SaveSecret() error = %v", err)
	}
	if ok, err := Exists(dir); err != nil || !ok {
		t.Errorf("Exists(vault with secret) = %v, %v; want true, nil", ok, err)
	}
}

func TestFailedSaveKeepsPreviousSecret(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permissions this test relies on")
	}
	dir := t.TempDir()
	v, err := Open(dir, fixedPrompt("passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := v.SaveSecret("feed", []byte("old")); err != nil {
		t.Fatalf("SaveSecret() error = %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := v.SaveSecret("feed", []byte("new")); err == nil {
		t.Fatal("SaveSecret() into a read-only vault succeeded")
	}
	got, err := v.LoadSecret("feed")
	if err != nil || string(got) != "old" {
		t.Errorf("LoadSecret() = %q, %v; want the previous secret", got, err)
	}
}

func TestSaveLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	v, err := Open(dir, fixedPrompt("passphrase"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, s := range []string{"one", "two"} {
		if err := v.SaveSecret("feed", []byte(s)); err != nil {
			t.Fatalf("SaveSecret() error = %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "feed"+secretExt {
			t.Errorf("unexpected file %q in vault", e.Name())
		}
		if info, err := e.Info(); err == nil && info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", e.Name(), info.Mode().Perm())
		}
	}
	if got, _ := v.LoadSecret("feed"); string(got) != "two" {
		t.Errorf("LoadSecret() = %q, want %q", got, "two")
	}
}
