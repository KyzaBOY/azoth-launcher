package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// platformResolveForms are the extra spellings ResolveGame accepts on Windows:
// a trailing backslash and another case.
func platformResolveForms(root, bin string) []string {
	return []string{root + `\`, strings.ToLower(bin)}
}

// mkHiddenInstall creates an install the scan must not find: a folder with the
// hidden attribute.
func mkHiddenInstall(t *testing.T, root string) {
	t.Helper()
	hidden := mkInstall(t, filepath.Join(root, "hidden"))
	hp, err := windows.UTF16PtrFromString(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(hp, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
}

// mkDirLink makes link a junction to target and removes the junction (never
// the target) at the end of the test.
func mkDirLink(t *testing.T, link, target string) {
	t.Helper()
	if err := CreateJunction(link, target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(link) })
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
