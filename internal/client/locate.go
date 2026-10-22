package client

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrGameNotFound is returned by ResolveGame when the folder has no
// Bin64/NewWorld.exe (directly or one level down).
var ErrGameNotFound = errors.New(filepath.Join("Bin64", ExeName) + " not found")

// IsolatedCopyDir is where the launcher builds its own isolated copy of the
// game (<dataDir>\client), with Bin64\ inside.
func IsolatedCopyDir(dataDir string) string {
	return filepath.Join(dataDir, "client")
}

// hasExe says whether dir\Bin64\NewWorld.exe exists (as a file).
func hasExe(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "Bin64", ExeName))
	return err == nil && !st.IsDir()
}

// ResolveGame turns the folder the player picked into the install root and its
// Bin64 folder. It accepts the root (the one with Bin64\NewWorld.exe), the
// Bin64 folder itself or the path of NewWorld.exe. Nothing is written.
func ResolveGame(path string) (root, binDir string, err error) {
	p := strings.TrimSpace(path)
	p = strings.TrimSpace(strings.Trim(p, `"`))
	abs, err := filepath.Abs(p)
	if err != nil || p == "" {
		return "", "", ErrGameNotFound
	}
	if strings.EqualFold(filepath.Base(abs), ExeName) {
		abs = filepath.Dir(abs)
	}
	if strings.EqualFold(filepath.Base(abs), "Bin64") {
		if st, err := os.Stat(filepath.Join(abs, ExeName)); err == nil && !st.IsDir() {
			return filepath.Dir(abs), abs, nil
		}
	}
	if hasExe(abs) {
		return abs, filepath.Join(abs, "Bin64"), nil
	}
	return "", "", ErrGameNotFound
}

var vdfPathRe = regexp.MustCompile(`"path"\s+"((?:[^"\\]|\\.)*)"`)

// FindSteamInstalls looks for installs of New World through Steam (read-only):
// every Steam root (steamRoots) -> steamapps/libraryfolders.vdf -> every
// library's steamapps/common/New World. It returns the root of EVERY install
// that has Bin64/NewWorld.exe, not only the first one.
func FindSteamInstalls() []string {
	roots := steamRoots()
	libs := append([]string(nil), roots...)
	for _, r := range roots {
		data, err := os.ReadFile(filepath.Join(r, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		libs = append(libs, libraryFolders(data)...)
	}
	var found []string
	seen := map[string]bool{}
	for _, lib := range libs {
		root := filepath.Join(lib, "steamapps", "common", "New World")
		key := pathKey(root)
		if seen[key] {
			continue
		}
		seen[key] = true
		if hasExe(root) {
			found = append(found, root)
		}
	}
	return found
}

// libraryFolders returns the "path" of every library in a libraryfolders.vdf.
// Windows paths in the file have their backslashes escaped (D:\\SteamLibrary).
func libraryFolders(vdf []byte) []string {
	var libs []string
	for _, m := range vdfPathRe.FindAllStringSubmatch(string(vdf), -1) {
		libs = append(libs, filepath.Clean(strings.ReplaceAll(m[1], `\\`, `\`)))
	}
	return libs
}
