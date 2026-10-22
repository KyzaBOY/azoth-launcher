package client

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// linkKind names the assets link in messages.
const linkKind = "junction"

// linkDir makes link an NTFS junction to target.
func linkDir(link, target string) error { return CreateJunction(link, target) }

// pathKey is the form two paths are compared in: Windows paths ignore case.
func pathKey(p string) string { return strings.ToLower(filepath.Clean(p)) }

func sameDir(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(filepath.Clean(a), `\??\`), filepath.Clean(b))
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
