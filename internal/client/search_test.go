package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func mustMkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func paths(in []Install) []string {
	var out []string
	for _, i := range in {
		out = append(out, strings.ToLower(i.Path))
	}
	sort.Strings(out)
	return out
}

func TestSearchScan(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	want := []string{
		mkInstall(t, filepath.Join(root, "direct")),
		mkInstall(t, filepath.Join(root, "a", "b", "c")),                             // depth 3: still found
		mkInstall(t, filepath.Join(root, "lib", "steamapps", "common", "New World")), // Steam library layout
		mkInstall(t, filepath.Join(root, "Amazon Games", "Library", "New World")),    // depth 3
	}

	// none of these may be found
	mkInstall(t, filepath.Join(root, "a", "b", "c", "d"))  // depth 4
	mkInstall(t, filepath.Join(root, "Windows", "x"))      // skipped by name
	mkInstall(t, filepath.Join(root, "WINDOWS"))           // skipped by name (any case)
	mkInstall(t, filepath.Join(root, "ProgramData", "x"))  // skipped by name
	mkInstall(t, filepath.Join(root, "$Recycle.Bin", "x")) // skipped by name
	mkInstall(t, filepath.Join(root, "System Volume Information"))
	skipme := mkInstall(t, filepath.Join(root, "skipme")) // in SearchOptions.Skip
	hidden := mkInstall(t, filepath.Join(root, "hidden")) // hidden attribute
	hp, err := windows.UTF16PtrFromString(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(hp, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	mkInstall(t, filepath.Join(outside, "game")) // reachable only through the junction
	if err := CreateJunction(filepath.Join(root, "linked"), outside); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(filepath.Join(root, "linked")) // the junction only, never the target

	got, complete, err := Search(context.Background(), SearchOptions{Roots: []string{root}, NoSteam: true, Skip: []string{skipme}})
	if err != nil || !complete {
		t.Fatalf("Search: complete=%v err=%v", complete, err)
	}
	var wantLow []string
	for _, w := range want {
		wantLow = append(wantLow, strings.ToLower(w))
	}
	sort.Strings(wantLow)
	if gp := paths(got); strings.Join(gp, "\n") != strings.Join(wantLow, "\n") {
		t.Fatalf("found:\n%s\nwant:\n%s", strings.Join(gp, "\n"), strings.Join(wantLow, "\n"))
	}
	for _, i := range got {
		if i.Supported {
			t.Fatalf("%s: a fake exe cannot be a supported build", i.Path)
		}
	}
}

func TestSearchDepth(t *testing.T) {
	root := t.TempDir()
	mkInstall(t, filepath.Join(root, "one"))
	deep := mkInstall(t, filepath.Join(root, "a", "b"))
	got, _, err := Search(context.Background(), SearchOptions{Roots: []string{root}, NoSteam: true, Depth: 1})
	if err != nil || len(got) != 1 || !strings.HasSuffix(got[0].Path, "one") {
		t.Fatalf("depth 1: %+v %v", got, err)
	}
	got, _, err = Search(context.Background(), SearchOptions{Roots: []string{root}, NoSteam: true, Depth: 2})
	if err != nil || len(got) != 2 {
		t.Fatalf("depth 2 (finds %s): %+v %v", deep, got, err)
	}
}

func TestSearchCancelAndTimeout(t *testing.T) {
	root := t.TempDir()
	mkInstall(t, filepath.Join(root, "game"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Search(ctx, SearchOptions{Roots: []string{root}, NoSteam: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}

	// the time cap is not an error: partial results and complete=false
	_, complete, err := Search(context.Background(), SearchOptions{Roots: []string{root}, NoSteam: true, Timeout: time.Nanosecond})
	if err != nil || complete {
		t.Fatalf("time cap: complete=%v err=%v", complete, err)
	}
}

func TestSortInstalls(t *testing.T) {
	in := []Install{{Path: `D:\b`}, {Path: `C:\a`, Supported: true}, {Path: `A:\z`}, {Path: `Z:\s`, Supported: true}}
	sortInstalls(in)
	var got []string
	for _, i := range in {
		got = append(got, i.Path)
	}
	if want := `C:\a Z:\s A:\z D:\b`; strings.Join(got, " ") != want {
		t.Fatalf("order = %v, want %s", got, want)
	}
}

func TestFixedDrives(t *testing.T) {
	drives := FixedDrives()
	if len(drives) == 0 {
		t.Fatal("no fixed drive found")
	}
	sys := strings.ToUpper(os.Getenv("SystemDrive")) + `\`
	found := false
	for _, d := range drives {
		if len(d) != 3 || d[1] != ':' || d[2] != '\\' {
			t.Fatalf("odd drive root %q", d)
		}
		if d == sys {
			found = true
		}
	}
	if !found {
		t.Fatalf("system drive %s missing from %v", sys, drives)
	}
}
