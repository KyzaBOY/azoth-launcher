package client

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// platformResolveForms are the extra spellings ResolveGame accepts on Linux: a
// trailing slash. Paths are case-sensitive, so another case is not the same folder.
func platformResolveForms(root, _ string) []string {
	return []string{root + "/"}
}

// mkHiddenInstall creates an install the scan must not find: a dot folder.
func mkHiddenInstall(t *testing.T, root string) {
	t.Helper()
	mkInstall(t, filepath.Join(root, ".hidden"))
}

// mkDirLink makes link a symlink to target.
func mkDirLink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestResolveGameIsCaseSensitive(t *testing.T) {
	root := mkInstall(t, filepath.Join(t.TempDir(), "New World"))
	if _, _, err := ResolveGame(strings.ToLower(root)); err == nil {
		t.Fatal("another case is another folder on Linux")
	}
}

// home makes a temp home folder for the test and returns its real path.
func home(t *testing.T) string {
	t.Helper()
	h, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", h)
	return h
}

func TestSteamRoots(t *testing.T) {
	h := home(t)
	native := mustMkdir(t, filepath.Join(h, ".local", "share", "Steam"))
	mustMkdir(t, filepath.Join(h, ".steam"))
	if err := os.Symlink(native, filepath.Join(h, ".steam", "steam")); err != nil { // as the Steam package does
		t.Fatal(err)
	}
	flatpak := mustMkdir(t, filepath.Join(h, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"))

	got := steamRoots()
	want := []string{native, flatpak}
	if !slices.Equal(got, want) {
		t.Fatalf("steamRoots = %v, want %v (the ~/.steam/steam link counts once)", got, want)
	}

	if err := os.RemoveAll(filepath.Join(h, ".var")); err != nil {
		t.Fatal(err)
	}
	if got := steamRoots(); !slices.Equal(got, []string{native}) {
		t.Fatalf("without the Flatpak: %v", got)
	}
}

func TestFindSteamInstallsLinux(t *testing.T) {
	h := home(t)
	steam := mustMkdir(t, filepath.Join(h, ".local", "share", "Steam"))
	inSteam := mkInstall(t, filepath.Join(steam, "steamapps", "common", "New World"))
	lib := filepath.Join(t.TempDir(), "My Games", "SteamLibrary") // a space, as in "/mnt/My Games"
	inLib := mkInstall(t, filepath.Join(lib, "steamapps", "common", "New World"))
	vdf := `"libraryfolders"
{
	"0"
	{
		"path"		"` + steam + `"
		"label"		""
	}
	"1"
	{
		"path"		"` + lib + `"
	}
	"2"
	{
		"path"		"/nowhere/at/all"
	}
}
`
	if err := os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindSteamInstalls()
	if want := []string{inSteam, inLib}; !slices.Equal(got, want) {
		t.Fatalf("FindSteamInstalls = %v, want %v", got, want)
	}
}

func TestLibraryFolders(t *testing.T) {
	vdf := []byte(`"path"		"/home/p/.local/share/Steam"
"path"		"D:\\SteamLibrary"
"path"	"/mnt/My Games/SteamLibrary"`)
	got := libraryFolders(vdf)
	want := []string{"/home/p/.local/share/Steam", `D:\SteamLibrary`, "/mnt/My Games/SteamLibrary"}
	if !slices.Equal(got, want) {
		t.Fatalf("libraryFolders = %q, want %q", got, want)
	}
}

func TestDiskMounts(t *testing.T) {
	mountinfo := `22 1 259:2 / / rw,relatime shared:1 - btrfs /dev/nvme0n1p2 rw
23 22 0:21 / /proc rw,nosuid shared:12 - proc proc rw
24 22 0:22 / /sys rw,nosuid shared:2 - sysfs sysfs rw
25 22 0:5 / /dev rw,nosuid shared:8 - devtmpfs devtmpfs rw
26 22 0:24 / /run rw,nosuid shared:13 - tmpfs tmpfs rw
27 26 0:51 / /run/media/p/USB rw shared:30 - vfat /dev/sdc1 rw
28 22 259:1 / /boot/efi rw shared:29 - vfat /dev/nvme0n1p1 rw
29 22 259:3 / /home rw,relatime shared:31 - ext4 /dev/nvme0n1p3 rw
30 22 8:1 / /mnt/Games\0402TB rw shared:32 - ext4 /dev/sda1 rw
31 22 8:17 / /mnt/win rw shared:33 - ntfs3 /dev/sdb1 rw
32 22 0:60 / /mnt/nas rw shared:34 - nfs4 nas:/share rw
33 29 0:61 / /home/p/.cache/doc rw shared:35 - fuse.portal portal rw
34 22 0:62 / /tmp rw shared:36 - tmpfs tmpfs rw
35 22 7:3 / /snap/core/1 ro shared:37 - squashfs /dev/loop3 ro
36 22 8:33 / /usr/local rw shared:38 - ext4 /dev/sdd1 rw
broken line without a separator
`
	got := diskMounts(strings.NewReader(mountinfo), systemDirs)
	want := []string{"/", "/home", "/mnt/Games 2TB", "/mnt/win"}
	if !slices.Equal(got, want) {
		t.Fatalf("diskMounts = %q, want %q", got, want)
	}
}

func TestUnescapeMount(t *testing.T) {
	for in, want := range map[string]string{
		`/mnt/My\040Games`: "/mnt/My Games",
		`/a\134b`:          `/a\b`,
		`/tab\011x`:        "/tab\tx",
		`/plain`:           "/plain",
		`/odd\04`:          `/odd\04`,  // too short to be an escape
		`/odd\999`:         `/odd\999`, // not octal
	} {
		if got := unescapeMount(in); got != want {
			t.Errorf("unescapeMount(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchRootsHasHomeAndRoot(t *testing.T) {
	h := home(t)
	roots := searchRoots()
	if len(roots) == 0 || roots[0] != h {
		t.Fatalf("the home folder must be the first scan root: %v", roots)
	}
	for _, r := range roots[1:] { // the mounts (the test's home lives in the temp folder)
		for _, s := range systemDirs {
			if isInside(r, s) {
				t.Fatalf("scan root %s is inside %s", r, s)
			}
		}
	}
}

// The kernel and runtime folders are always in the scan's skip list.
func TestSearchSkipsSystemDirs(t *testing.T) {
	skip := map[string]bool{}
	for _, s := range alwaysSkip() {
		skip[pathKey(s)] = true
	}
	for _, s := range []string{"/proc", "/sys", "/dev", "/run", "/tmp"} {
		if !skip[s] {
			t.Fatalf("%s must always be skipped", s)
		}
	}
}

func TestLinkDirIsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := mustMkdir(t, filepath.Join(dir, "install", "assets"))
	link := filepath.Join(dir, "client", "assets")
	mustMkdir(t, filepath.Dir(link))
	if err := linkDir(link, target); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(link)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("assets must be a symlink: %v %v", st, err)
	}
	if cur, _ := os.Readlink(link); cur != target {
		t.Fatalf("link -> %s, want the absolute %s", cur, target)
	}
}
