package client

import (
	"io/fs"
	"syscall"

	"golang.org/x/sys/windows"
)

// searchRoots are the fixed drives.
func searchRoots() []string { return FixedDrives() }

// alwaysSkip lists folders the scan never enters besides skipNames: none on
// Windows, where skipNames and the hidden/system attributes cover it.
func alwaysSkip() []string { return nil }

// FixedDrives lists the roots (like `C:\`) of the fixed local drives: no
// removable media, network shares or optical drives.
func FixedDrives() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var drives []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if windows.GetDriveType(p) == windows.DRIVE_FIXED {
			drives = append(drives, root)
		}
	}
	return drives
}

// walkable says whether the scan may enter a directory entry: it must be a
// real directory, not hidden, not a system folder and not a reparse point
// (junction or symlink).
func walkable(e fs.DirEntry) bool {
	info, err := e.Info()
	if err != nil {
		return false
	}
	if attrs, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		const skipAttrs = syscall.FILE_ATTRIBUTE_HIDDEN | syscall.FILE_ATTRIBUTE_SYSTEM | syscall.FILE_ATTRIBUTE_REPARSE_POINT
		return attrs.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 && attrs.FileAttributes&skipAttrs == 0
	}
	return info.IsDir() && info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}
