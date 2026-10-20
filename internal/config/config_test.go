package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	if got, want := DataDir(), filepath.Join(base, "AzothLauncher"); got != want {
		t.Fatalf("DataDir = %q, want %q", got, want)
	}
	if got, want := Path(), filepath.Join(base, "AzothLauncher", "config.json"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	c, err := Load()
	if err != nil || c != (Config{}) {
		t.Fatalf("no file: %+v %v", c, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	want := Config{GameDir: `D:\Games\New World`}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if _, err := os.Stat(Path() + ".tmp"); err == nil {
		t.Fatal("the .tmp file was left behind")
	}

	// the file holds exactly one key: nothing else is persisted
	raw, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(strings.Fields(string(raw)), " "); got != `{ "gameDir": "D:\\Games\\New World" }` {
		t.Fatalf("config.json = %s", raw)
	}

	// overwrite
	want.GameDir = `D:\Games\New World`
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(); err != nil || got != want {
		t.Fatalf("after overwrite: %+v %v", got, err)
	}
}

func TestLoadIgnoresUnknownKeysAndRejectsGarbage(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// keys from older versions are ignored
	if err := os.WriteFile(Path(), []byte(`{"gameDir":"C:\\NW","gatewayHost":"x","menuPing":"icmp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err != nil || c.GameDir != `C:\NW` {
		t.Fatalf("old keys: %+v %v", c, err)
	}
	if err := os.WriteFile(Path(), []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(); err == nil || c != (Config{}) {
		t.Fatalf("garbage: %+v %v", c, err)
	}
}
