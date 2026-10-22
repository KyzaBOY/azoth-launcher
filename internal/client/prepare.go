package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// PrepareOptions describes the building of an isolated copy of the client.
type PrepareOptions struct {
	GameDir string       // install root (READ-ONLY), e.g. D:\Games\New World
	Dest    string       // root of the copy, e.g. %LOCALAPPDATA%\AzothLauncher\client
	Log     func(string) // steps as text (optional)
}

// Prepare builds the isolated copy in Dest: Bin64 copied byte for byte from
// the install (no patch on disk, the patches are applied in memory at launch),
// assets as a link to the install (a junction on Windows, a symlink on Linux;
// no admin needed) and the root config files copied. The install is not changed.
func Prepare(opts PrepareOptions) (string, error) {
	logf := func(format string, a ...any) {
		if opts.Log != nil {
			opts.Log(fmt.Sprintf(format, a...))
		}
	}
	game, err := filepath.Abs(opts.GameDir)
	if err != nil {
		return "", err
	}
	dest, err := filepath.Abs(opts.Dest)
	if err != nil {
		return "", err
	}
	if isInside(dest, game) || isInside(game, dest) {
		return "", errors.New("the destination cannot be inside the install (nor the other way around)")
	}
	srcBin := filepath.Join(game, "Bin64")
	srcExe := filepath.Join(srcBin, ExeName)
	if _, err := os.Stat(srcExe); err != nil {
		return "", fmt.Errorf("install without %s: %s", filepath.Join("Bin64", ExeName), game)
	}

	logf("checking the install build (sha256 of %s)...", srcExe)
	sum, _, err := NormalizedSHA256(srcExe)
	if err != nil {
		return "", err
	}
	if sum != ExpectedSHA256 {
		return "", fmt.Errorf("%w: sha256 = %s (expected %s)", ErrUnsupportedBuild, sum, ExpectedSHA256)
	}
	logf("supported build (sha256 %s...)", sum[:16])

	dstBin := filepath.Join(dest, "Bin64")
	if err := os.MkdirAll(dstBin, 0o755); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(srcBin)
	if err != nil {
		return "", err
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if st, err := e.Info(); err == nil {
			total += st.Size()
		}
	}
	logf("copying Bin64 (%d files, %.0f MB) -> %s", len(entries), float64(total)/1e6, dstBin)
	var done int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(srcBin, e.Name())
		dst := filepath.Join(dstBin, e.Name())
		n, err := copyFileIfDifferent(src, dst)
		if err != nil {
			return "", fmt.Errorf("copy %s: %w", e.Name(), err)
		}
		done += n
		logf("  %-28s %8.1f MB  (%3.0f%%)", e.Name(), float64(n)/1e6, 100*float64(done)/float64(max(total, 1)))
	}

	for _, f := range rootFiles {
		src := filepath.Join(game, f)
		if _, err := os.Stat(src); err != nil {
			logf("  warning: %s does not exist in the install, skipping", f)
			continue
		}
		if _, err := copyFileIfDifferent(src, filepath.Join(dest, f)); err != nil {
			return "", fmt.Errorf("copy %s: %w", f, err)
		}
		logf("  %s copied", f)
	}

	autoexec := filepath.Join(dest, "autoexec.cfg")
	if _, err := os.Stat(autoexec); err != nil {
		if err := os.WriteFile(autoexec, []byte("-- Azoth: extra client cvars, one per line (name = value)\n"), 0o644); err != nil {
			return "", err
		}
		logf("  autoexec.cfg created (empty)")
	}

	if err := ensureAssetsLink(filepath.Join(dest, "assets"), filepath.Join(game, "assets"), logf); err != nil {
		return "", err
	}

	logf("isolated copy ready at %s (the exe was NOT patched on disk; the patches are in memory)", dest)
	return dstBin, nil
}

// ensureAssetsLink makes link a directory link (linkKind) to target. A link
// that already points there stays; one that points somewhere else (the player
// moved or switched installs) is replaced; a real folder or file is left alone
// and reported.
func ensureAssetsLink(link, target string, logf func(string, ...any)) error {
	st, err := os.Lstat(link)
	if err != nil {
		logf("  creating assets %s -> %s", linkKind, target)
		if err := linkDir(link, target); err != nil {
			return fmt.Errorf("assets %s: %w", linkKind, err)
		}
		return nil
	}
	if st.Mode()&os.ModeSymlink == 0 && st.Mode()&os.ModeIrregular == 0 {
		return fmt.Errorf("%s exists and is not a %s; remove it first", link, linkKind)
	}
	if cur, err := os.Readlink(link); err == nil && sameDir(cur, target) {
		logf("  assets %s already exists", linkKind)
		return nil
	}
	logf("  assets %s points elsewhere; pointing it to %s", linkKind, target)
	if err := os.Remove(link); err != nil { // removes the link only, never what it points to
		return fmt.Errorf("assets %s: %w", linkKind, err)
	}
	if err := linkDir(link, target); err != nil {
		return fmt.Errorf("assets %s: %w", linkKind, err)
	}
	return nil
}

// CopyUpToDate says whether dest already holds a complete isolated copy of the
// install at gameDir: the copied exe has the same size as the install's and the
// supported (normalized) sha256, and the assets link points at the install.
// Anything else (missing, other size or hash, other install) means Prepare has
// to run again.
func CopyUpToDate(gameDir, dest string) bool {
	src, err := os.Stat(filepath.Join(gameDir, "Bin64", ExeName))
	if err != nil {
		return false
	}
	exe := filepath.Join(dest, "Bin64", ExeName)
	cp, err := os.Stat(exe)
	if err != nil || cp.Size() != src.Size() {
		return false
	}
	link := filepath.Join(dest, "assets")
	if st, err := os.Lstat(link); err != nil || (st.Mode()&os.ModeSymlink == 0 && st.Mode()&os.ModeIrregular == 0) {
		return false
	}
	if cur, err := os.Readlink(link); err != nil || !sameDir(cur, filepath.Join(gameDir, "assets")) {
		return false
	}
	sum, _, err := NormalizedSHA256(exe)
	return err == nil && sum == ExpectedSHA256
}

// copyFileIfDifferent copies src to dst unless dst already exists with the
// same size and mtime (running the preparation again is cheap).
func copyFileIfDifferent(src, dst string) (int64, error) {
	sst, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	if dst, err := os.Stat(dst); err == nil && dst.Size() == sst.Size() && dst.ModTime().Equal(sst.ModTime()) {
		return sst.Size(), nil
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	cerr := out.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	_ = os.Chtimes(dst, sst.ModTime(), sst.ModTime())
	return n, nil
}

// isInside says whether path is dir itself or lies below it.
func isInside(path, dir string) bool {
	sep := string(filepath.Separator)
	return strings.HasPrefix(pathKey(path)+sep, pathKey(dir)+sep)
}

// PathInside says whether path is dir itself or lies below it, compared the
// way the system compares paths (case-insensitive on Windows).
func PathInside(path, dir string) bool { return isInside(path, dir) }

// SamePath says whether a and b name the same folder, compared the way the
// system compares paths.
func SamePath(a, b string) bool { return pathKey(a) == pathKey(b) }

// isLink says whether st (from os.Lstat) is a junction or a symlink.
func isLink(st os.FileInfo) bool {
	return st.Mode()&os.ModeSymlink != 0 || st.Mode()&os.ModeIrregular != 0
}

// RemoveCopy deletes the isolated copy in dir, the way Prepare built it: the
// assets link (the link only, never the install behind it), Bin64 and the
// files at its root (the config files, autoexec.cfg and the game's
// Default.log). Anything else in dir stays, and so does dir if it is not empty
// then. A folder that is not a copy (no Bin64/NewWorld.exe, an install, or
// assets as a real folder) is refused before anything is deleted.
func RemoveCopy(dir string) error {
	if !hasExe(dir) || IsSteamInstall(filepath.Join(dir, "Bin64")) {
		return fmt.Errorf("%s is not an isolated copy", dir)
	}
	link := filepath.Join(dir, "assets")
	if st, err := os.Lstat(link); err == nil {
		if !isLink(st) {
			return fmt.Errorf("%s is not a %s; the copy was left alone", link, linkKind)
		}
		if err := os.Remove(link); err != nil { // the link only
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, "Bin64")); err != nil {
		return err
	}
	for _, f := range append(append([]string{}, rootFiles...), "autoexec.cfg", "Default.log") {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	_ = os.Remove(dir) // fails, and keeps it, when something else is still inside
	return nil
}
