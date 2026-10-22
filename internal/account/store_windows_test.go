package account

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "AzothLauncher") // Save creates the directory
	if _, ok := Load(dir); ok {
		t.Fatal("no file means no session")
	}
	want := Session{Token: "secret-token-123", Expires: time.Now().Add(time.Hour).Unix(), User: User{ID: "42", Name: "alice"}}
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-token-123")) || bytes.Contains(raw, []byte("alice")) {
		t.Fatal("account.dat is in plain text")
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); err == nil {
		t.Fatal("the .tmp file was left behind")
	}
	got, ok := Load(dir)
	if !ok || got != want {
		t.Fatalf("round trip = %+v %v", got, ok)
	}

	// overwrite (logging in again)
	want2 := Session{Token: "other", Expires: want.Expires + 10, User: User{ID: "43", Name: "bob"}}
	if err := Save(dir, want2); err != nil {
		t.Fatal(err)
	}
	if got, ok := Load(dir); !ok || got != want2 {
		t.Fatalf("after overwriting = %+v %v", got, ok)
	}

	// expired
	if err := Save(dir, Session{Token: "old", Expires: time.Now().Add(-time.Minute).Unix(), User: User{ID: "42"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("an expired session must not load")
	}

	// corrupted
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("this is not dpapi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("a corrupted file must not load")
	}

	// Clear: deletes, and does not complain about a missing file
	if err := Save(dir, want); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := Load(dir); ok {
		t.Fatal("no session after Clear")
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear without a file: %v", err)
	}
}
