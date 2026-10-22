package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The normalized hash must be the same for the original copy, for a copy with
// one patch and for a copy with both patches on disk, and equal to the plain
// sha256 of the original file. Odd bytes at a spot do not normalize.
func TestNormalizedSHA256(t *testing.T) {
	sites := []patchSite{
		{Name: "ping", Offset: 900, Orig: PingOrigBytes[:], Patched: PingPatchedBytes[:]}, // out of order on purpose
		{Name: "dtls", Offset: 37, Orig: OrigBytes[:], Patched: PatchedBytes[:]},
	}
	body := make([]byte, 4096)
	for i := range body {
		body[i] = byte(i * 7)
	}
	mk := func(dtls, ping []byte) []byte {
		b := append([]byte(nil), body...)
		copy(b[37:], dtls)
		copy(b[900:], ping)
		return b
	}
	orig := mk(OrigBytes[:], PingOrigBytes[:])
	pure := sha256.Sum256(orig)
	want := hex.EncodeToString(pure[:])

	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name       string
		data       []byte
		dtls, ping bool
	}{
		{"original", orig, false, false},
		{"only-dtls", mk(PatchedBytes[:], PingOrigBytes[:]), true, false},
		{"only-ping", mk(OrigBytes[:], PingPatchedBytes[:]), false, true},
		{"both", mk(PatchedBytes[:], PingPatchedBytes[:]), true, true},
	}
	for _, c := range cases {
		sum, patched, err := normalizedSHA256(write(c.name+".bin", c.data), sites)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if sum != want {
			t.Fatalf("%s: sum=%s want %s", c.name, sum, want)
		}
		if patched["dtls"] != c.dtls || patched["ping"] != c.ping {
			t.Fatalf("%s: patched=%v", c.name, patched)
		}
	}

	other := mk(OrigBytes[:], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	sum, patched, err := normalizedSHA256(write("other.bin", other), sites)
	if err != nil || len(patched) != 0 || sum == want {
		t.Fatalf("odd bytes must not normalize: sum=%s patched=%v err=%v", sum, patched, err)
	}
	if _, _, err := normalizedSHA256(write("short.bin", body[:100]), sites); !errors.Is(err, ErrTooShort) {
		t.Fatalf("a short file must fail with ErrTooShort, got %v", err)
	}
}

// The numbers agree with each other: .text has VA 0x1000/raw 0x600 and .rdata
// VA 0x7ec9000/raw 0x7ec7800 (checked in the PE); the pinger pointer in the
// file is the preferred base + the string's RVA.
func TestPatchNumbersAreConsistent(t *testing.T) {
	if uint64(PatchFileOffset)+(0x1000-0x600) != PatchRVA {
		t.Fatalf("dtls: RVA 0x%x != offset 0x%x + 0xA00", PatchRVA, PatchFileOffset)
	}
	if uint64(PingPatchFileOffset)+(0x7ec9000-0x7ec7800) != PingPatchRVA {
		t.Fatalf("ping: RVA 0x%x != offset 0x%x + 0x1800", PingPatchRVA, PingPatchFileOffset)
	}
	if PingHTTPSRVA-PingICMPRVA != 0x10 {
		t.Fatal("icmp must sit 16 bytes before https")
	}
	le := func(b [PingPatchLen]byte) uint64 {
		var v uint64
		for i := PingPatchLen - 1; i >= 0; i-- {
			v = v<<8 | uint64(b[i])
		}
		return v
	}
	if le(PingOrigBytes) != PreferredImageBase+PingHTTPSRVA || le(PingPatchedBytes) != PreferredImageBase+PingICMPRVA {
		t.Fatalf("pointer bytes: orig=0x%x patched=0x%x", le(PingOrigBytes), le(PingPatchedBytes))
	}
	if len(PatchSites) != 2 || PatchSites[0].Offset >= PatchSites[1].Offset {
		t.Fatal("PatchSites must be in offset order")
	}
}

func TestGameArgs(t *testing.T) {
	got := strings.Join(GameArgs("play.example.test:6000"), " ")
	want := "-noprompt -ProductId dev -AuthManager sonic -AuthMode none -GatewayMode stubbed -GatewayAddr play.example.test:6000"
	if got != want {
		t.Fatalf("args:\n got %s\nwant %s", got, want)
	}
	if strings.Contains(got, "OverrideConfigPath") {
		t.Fatal("the file override is gone")
	}
}

func TestIsSteamInstall(t *testing.T) {
	for _, p := range []string{
		`D:\SteamLibrary\steamapps\common\New World\Bin64`,
		"/home/player/.local/share/Steam/steamapps/common/New World/Bin64",
	} {
		if !IsSteamInstall(p) {
			t.Fatalf("a Steam path must be refused: %s", p)
		}
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "Bin64")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if IsSteamInstall(bin) {
		t.Fatal("a clean copy is not an install")
	}
	if err := os.WriteFile(filepath.Join(dir, "installscript.vdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsSteamInstall(bin) {
		t.Fatal("installscript.vdf at the root marks the install")
	}

	// an EasyAntiCheat folder at the root marks an install too (non-Steam installs have it)
	dir2 := t.TempDir()
	bin2 := filepath.Join(dir2, "Bin64")
	if err := os.MkdirAll(filepath.Join(dir2, "EasyAntiCheat"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin2, 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsSteamInstall(bin2) {
		t.Fatal("EasyAntiCheat at the root marks the install")
	}
}

// mkInstall creates root\Bin64\NewWorld.exe (any content) and returns root.
func mkInstall(t *testing.T, root string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "Bin64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Bin64", ExeName), []byte("not a real exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveGame(t *testing.T) {
	root := mkInstall(t, filepath.Join(t.TempDir(), "New World"))
	bin := filepath.Join(root, "Bin64")

	for _, in := range append([]string{
		root,
		`"` + root + `"`,
		"  " + root + "  ",
		bin,
		filepath.Join(bin, ExeName),
	}, platformResolveForms(root, bin)...) {
		gotRoot, gotBin, err := ResolveGame(in)
		if err != nil || !sameDir(gotRoot, root) || !sameDir(gotBin, bin) {
			t.Fatalf("ResolveGame(%q) = %q, %q, %v", in, gotRoot, gotBin, err)
		}
	}

	empty := t.TempDir()
	for _, in := range []string{"", "   ", filepath.Join(t.TempDir(), "nope"), empty, filepath.Join(empty, "Bin64")} {
		if _, _, err := ResolveGame(in); !errors.Is(err, ErrGameNotFound) {
			t.Fatalf("ResolveGame(%q) = %v, want ErrGameNotFound", in, err)
		}
	}
}

func TestEnsureAssetsLink(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "installA", "assets")
	b := filepath.Join(dir, "installB", "assets")
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "marker.txt"), []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	logf := func(string, ...any) {}
	link := filepath.Join(dir, "copy", "assets")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ensureAssetsLink(link, a, logf); err != nil {
		t.Fatal(err)
	}
	if cur, err := os.Readlink(link); err != nil || !sameDir(cur, a) {
		t.Fatalf("link -> %q (%v), want %q", cur, err, a)
	}
	// same target: nothing to do
	if err := ensureAssetsLink(link, a, logf); err != nil {
		t.Fatal(err)
	}
	// another install: the link is retargeted, never the folders behind it
	if err := ensureAssetsLink(link, b, logf); err != nil {
		t.Fatal(err)
	}
	if cur, err := os.Readlink(link); err != nil || !sameDir(cur, b) {
		t.Fatalf("link -> %q (%v), want %q", cur, err, b)
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(filepath.Join(p, "marker.txt")); err != nil {
			t.Fatalf("%s lost its content: %v", p, err)
		}
	}

	// a real folder is not a link: refuse instead of replacing it
	real := filepath.Join(dir, "copy2", "assets")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureAssetsLink(real, a, logf); err == nil {
		t.Fatal("a real folder must not be replaced")
	}

	// remove the link itself (not its target) before the temp dir cleanup
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, "marker.txt")); err != nil {
		t.Fatalf("removing the link touched its target: %v", err)
	}
}

func TestCopyUpToDateWithoutACopy(t *testing.T) {
	game := mkInstall(t, filepath.Join(t.TempDir(), "New World"))
	if CopyUpToDate(game, filepath.Join(t.TempDir(), "client")) {
		t.Fatal("a missing copy cannot be up to date")
	}
	// a copy whose exe differs from the install's (here: another size) is stale
	dest := t.TempDir()
	mkInstall(t, dest)
	if err := os.WriteFile(filepath.Join(dest, "Bin64", ExeName), []byte("something else entirely"), 0o644); err != nil {
		t.Fatal(err)
	}
	if CopyUpToDate(game, dest) {
		t.Fatal("a different exe cannot be up to date")
	}
}

// TestPrepareRealInstall builds an isolated copy of the real install when
// AZOTH_CLIENT_BIN points at its Bin64 folder (about 270 MB is copied to the
// temp folder) and checks that the copy then counts as up to date.
func TestPrepareRealInstall(t *testing.T) {
	bin := os.Getenv("AZOTH_CLIENT_BIN")
	if bin == "" {
		t.Skip("AZOTH_CLIENT_BIN not set")
	}
	game := filepath.Dir(bin)
	dest := filepath.Join(t.TempDir(), "client")
	got, err := Prepare(PrepareOptions{GameDir: game, Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	if !sameDir(got, filepath.Join(dest, "Bin64")) {
		t.Fatalf("Prepare returned %s", got)
	}
	if !CopyUpToDate(game, dest) {
		t.Fatal("a fresh copy must be up to date")
	}
	if IsSteamInstall(got) {
		t.Fatal("the copy must not look like an install")
	}
	if err := os.Remove(filepath.Join(dest, "assets")); err != nil { // the link only, never the install's assets
		t.Fatal(err)
	}
}
