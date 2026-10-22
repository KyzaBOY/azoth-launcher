package account

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeKeyring is a working Secret Service in memory.
type fakeKeyring map[string][]byte

func (f fakeKeyring) get(dir string) ([]byte, error) {
	v, ok := f[dir]
	if !ok {
		return nil, errNoSecret
	}
	return v, nil
}
func (f fakeKeyring) set(dir string, v []byte) error { f[dir] = v; return nil }
func (f fakeKeyring) delete(dir string) error        { delete(f, dir); return nil }

// noKeyring is a session bus without a Secret Service.
type noKeyring struct{}

func (noKeyring) get(string) ([]byte, error) { return nil, errNoService }
func (noKeyring) set(string, []byte) error   { return errNoService }
func (noKeyring) delete(string) error        { return errNoService }

func useKeyring(t *testing.T, k secretStore) {
	t.Helper()
	old := keyring
	keyring = k
	t.Cleanup(func() { keyring = old })
}

func testSession() Session {
	return Session{Token: "secret-token-123", Expires: time.Now().Add(time.Hour).Unix(), User: User{ID: "42", Name: "alice"}}
}

func TestStoreKeyring(t *testing.T) {
	k := fakeKeyring{}
	useKeyring(t, k)
	dir := filepath.Join(t.TempDir(), "AzothLauncher")

	// a session from before there was a keyring
	if err := writeAccountFile(dir, []byte(`{"session":"old"}`)); err != nil {
		t.Fatal(err)
	}
	want := testSession()
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("with a keyring no account.dat may stay on disk: %v", err)
	}
	if got, ok := Load(dir); !ok || got != want {
		t.Fatalf("round trip = %+v %v", got, ok)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if len(k) != 0 {
		t.Fatal("Clear must delete the keyring item")
	}
	if _, ok := Load(dir); ok {
		t.Fatal("no session after Clear")
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear without a session: %v", err)
	}
}

func TestStoreFileFallback(t *testing.T) {
	useKeyring(t, noKeyring{})
	dir := filepath.Join(t.TempDir(), "AzothLauncher")
	if _, ok := Load(dir); ok {
		t.Fatal("no file means no session")
	}
	want := testSession()
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("account.dat has mode %v, want 0600", st.Mode().Perm())
	}
	if got, ok := Load(dir); !ok || got != want {
		t.Fatalf("round trip = %+v %v", got, ok)
	}

	// expired
	if err := Save(dir, Session{Token: "old", Expires: time.Now().Add(-time.Minute).Unix(), User: User{ID: "42"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("an expired session must not load")
	}
	// corrupted
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("a corrupted file must not load")
	}

	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Clear must delete account.dat: %v", err)
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear without a file: %v", err)
	}
}

// A keyring that fails for another reason (the person refused to unlock it)
// is an error: the session is NOT silently written to disk instead.
func TestStoreKeyringRefusedDoesNotFallBack(t *testing.T) {
	useKeyring(t, refusedKeyring{})
	dir := t.TempDir()
	if err := Save(dir, testSession()); err == nil {
		t.Fatal("a refused keyring must fail the save")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("nothing may be written to disk")
	}
}

type refusedKeyring struct{}

var errRefused = errors.New("the keyring was not unlocked")

func (refusedKeyring) get(string) ([]byte, error) { return nil, errRefused }
func (refusedKeyring) set(string, []byte) error   { return errRefused }
func (refusedKeyring) delete(string) error        { return errRefused }

// TestSecretServiceReal stores, reads and deletes an item in the real keyring
// of this session, when AZOTH_KEYRING_TEST=1. The item is tied to a temp
// directory, so the launcher's own session is never touched.
func TestSecretServiceReal(t *testing.T) {
	if os.Getenv("AZOTH_KEYRING_TEST") != "1" {
		t.Skip("AZOTH_KEYRING_TEST not set")
	}
	var s secretService
	dir := t.TempDir()
	secret := []byte(`{"session":"keyring-test"}`)
	if err := s.set(dir, secret); err != nil {
		t.Fatal(err)
	}
	got, err := s.get(dir)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("get = %q %v", got, err)
	}
	if err := s.delete(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := s.get(dir); !errors.Is(err, errNoSecret) {
		t.Fatalf("after delete: %v", err)
	}
}
