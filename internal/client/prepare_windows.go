package client

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrepareOptions describes the building of an isolated copy of the client.
type PrepareOptions struct {
	GameDir string       // install root (READ-ONLY), e.g. D:\Games\New World
	Dest    string       // root of the copy, e.g. %LOCALAPPDATA%\AzothLauncher\client
	Log     func(string) // steps as text (optional)
}

// Prepare builds the isolated copy in Dest: Bin64\ copied byte for byte from
// the install (no patch on disk, the patches are applied in memory at launch),
// assets as a junction to the install (no admin needed) and the root config
// files copied. The install is not changed.
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
	if strings.HasPrefix(strings.ToLower(dest)+`\`, strings.ToLower(game)+`\`) || strings.HasPrefix(strings.ToLower(game)+`\`, strings.ToLower(dest)+`\`) {
		return "", errors.New("the destination cannot be inside the install (nor the other way around)")
	}
	srcBin := filepath.Join(game, "Bin64")
	srcExe := filepath.Join(srcBin, ExeName)
	if _, err := os.Stat(srcExe); err != nil {
		return "", fmt.Errorf("install without Bin64\\%s: %s", ExeName, game)
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

// ensureAssetsLink makes link a junction to target. A junction that already
// points there stays; one that points somewhere else (the player moved or
// switched installs) is replaced; a real folder or file is left alone and
// reported.
func ensureAssetsLink(link, target string, logf func(string, ...any)) error {
	st, err := os.Lstat(link)
	if err != nil {
		logf("  creating assets junction -> %s", target)
		if err := CreateJunction(link, target); err != nil {
			return fmt.Errorf("assets junction: %w", err)
		}
		return nil
	}
	if !isLink(st) {
		return fmt.Errorf("%s exists and is not a junction; remove it first", link)
	}
	if cur, err := os.Readlink(link); err == nil && sameDir(cur, target) {
		logf("  assets junction already exists")
		return nil
	}
	logf("  assets junction points elsewhere; pointing it to %s", target)
	if err := os.Remove(link); err != nil { // removes the junction only, never what it points to
		return fmt.Errorf("assets junction: %w", err)
	}
	if err := CreateJunction(link, target); err != nil {
		return fmt.Errorf("assets junction: %w", err)
	}
	return nil
}

// isLink says whether st (from os.Lstat) is a junction or a symlink.
func isLink(st os.FileInfo) bool {
	return st.Mode()&os.ModeSymlink != 0 || st.Mode()&os.ModeIrregular != 0
}

func sameDir(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(filepath.Clean(a), `\??\`), filepath.Clean(b))
}

// CopyUpToDate says whether dest already holds a complete isolated copy of the
// install at gameDir: the copied exe has the same size as the install's and the
// supported (normalized) sha256, and the assets junction points at the install.
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
	if st, err := os.Lstat(link); err != nil || !isLink(st) {
		return false
	}
	if cur, err := os.Readlink(link); err != nil || !sameDir(cur, filepath.Join(gameDir, "assets")) {
		return false
	}
	sum, _, err := NormalizedSHA256(exe)
	return err == nil && sum == ExpectedSHA256
}

// RemoveCopy deletes the isolated copy in dir, the way Prepare built it: the
// assets junction (the link only, never the install behind it), Bin64 and the
// files at its root (the config files, autoexec.cfg and the game's
// Default.log). Anything else in dir stays, and so does dir if it is not empty
// then. A folder that is not a copy (no Bin64\NewWorld.exe, an install, or
// assets as a real folder) is refused before anything is deleted.
func RemoveCopy(dir string) error {
	if !hasExe(dir) || IsSteamInstall(filepath.Join(dir, "Bin64")) {
		return fmt.Errorf("%s is not an isolated copy", dir)
	}
	link := filepath.Join(dir, "assets")
	if st, err := os.Lstat(link); err == nil {
		if !isLink(st) {
			return fmt.Errorf("%s is not a junction; the copy was left alone", link)
		}
		if err := os.Remove(link); err != nil { // the junction only
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

// CreateJunction creates an NTFS junction (mount point) link -> target, like
// `mklink /J`, without admin privileges: an empty folder plus
// FSCTL_SET_REPARSE_POINT with a REPARSE_DATA_BUFFER of tag
// IO_REPARSE_TAG_MOUNT_POINT. If the native call fails, it tries mklink /J itself.
func CreateJunction(link, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.Mkdir(link, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := setMountPoint(link, absTarget); err != nil {
		cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", link, absTarget)
		if out, cerr := cmd.CombinedOutput(); cerr != nil {
			_ = os.Remove(link)
			return fmt.Errorf("native: %v; mklink: %v (%s)", err, cerr, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func setMountPoint(link, absTarget string) error {
	linkPtr, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(linkPtr, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("CreateFile: %w", err)
	}
	defer windows.CloseHandle(h)

	subst, err := windows.UTF16FromString(`\??\` + absTarget) // ends in NUL
	if err != nil {
		return err
	}
	print, err := windows.UTF16FromString(absTarget)
	if err != nil {
		return err
	}
	substLen := uint16((len(subst) - 1) * 2) // bytes, without the NUL
	printLen := uint16((len(print) - 1) * 2)
	pathBuf := append(append([]uint16{}, subst...), print...) // subst\0print\0

	// REPARSE_DATA_BUFFER: tag(4) len(2) reserved(2) + MountPointReparseBuffer (4x uint16 + PathBuffer)
	hdr := 8
	mp := 8
	buf := make([]byte, hdr+mp+len(pathBuf)*2)
	putU32 := func(off int, v uint32) {
		buf[off], buf[off+1], buf[off+2], buf[off+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	}
	putU16 := func(off int, v uint16) { buf[off], buf[off+1] = byte(v), byte(v>>8) }
	putU32(0, windows.IO_REPARSE_TAG_MOUNT_POINT)
	putU16(4, uint16(mp+len(pathBuf)*2))
	putU16(6, 0)
	putU16(8, 0)           // SubstituteNameOffset
	putU16(10, substLen)   // SubstituteNameLength
	putU16(12, substLen+2) // PrintNameOffset (after the NUL)
	putU16(14, printLen)   // PrintNameLength
	copy(buf[hdr+mp:], unsafe.Slice((*byte)(unsafe.Pointer(&pathBuf[0])), len(pathBuf)*2))

	var ret uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &ret, nil); err != nil {
		return fmt.Errorf("DeviceIoControl(FSCTL_SET_REPARSE_POINT): %w", err)
	}
	return nil
}
