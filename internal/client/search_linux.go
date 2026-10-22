package client

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// mountInfoPath lists the mounts of this process (see proc(5)).
const mountInfoPath = "/proc/self/mountinfo"

// diskFileSystems are the file system types a game can be installed on. Every
// other type (proc, sysfs, tmpfs, overlay, fuse portals, network shares...) is
// not a scan root.
var diskFileSystems = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "btrfs": true, "xfs": true, "f2fs": true,
	"ntfs": true, "ntfs3": true, "fuseblk": true, "exfat": true, "vfat": true,
	"zfs": true, "bcachefs": true, "jfs": true, "reiserfs": true,
}

// systemDirs are never entered: kernel and runtime file systems, boot
// partitions and the system's own programs.
var systemDirs = []string{"/proc", "/sys", "/dev", "/run", "/tmp", "/snap", "/boot", "/efi", "/usr"}

// alwaysSkip lists folders the scan never enters besides skipNames.
func alwaysSkip() []string { return systemDirs }

// searchRoots are the home folder and every disk mount outside systemDirs.
func searchRoots() []string {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return roots
	}
	defer f.Close()
	return append(roots, diskMounts(f, systemDirs)...)
}

// diskMounts reads a mountinfo file and returns the mount points whose file
// system is in diskFileSystems and that are not inside one of skip.
func diskMounts(r io.Reader, skip []string) []string {
	var mounts []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
		pre, post, ok := strings.Cut(sc.Text(), " - ")
		if !ok {
			continue
		}
		fields, fsFields := strings.Fields(pre), strings.Fields(post)
		if len(fields) < 5 || len(fsFields) < 1 || !diskFileSystems[fsFields[0]] {
			continue
		}
		mount := unescapeMount(fields[4])
		inSkip := false
		for _, s := range skip {
			if isInside(mount, s) {
				inSkip = true
				break
			}
		}
		if !inSkip {
			mounts = append(mounts, mount)
		}
	}
	return mounts
}

// unescapeMount turns the octal escapes of mountinfo (\040 for a space,
// \011, \012, \134) back into their characters.
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// walkable says whether the scan may enter a directory entry: it must be a
// real directory (a symlink is not) and not hidden (a name that starts with a dot).
func walkable(e fs.DirEntry) bool {
	return e.IsDir() && !strings.HasPrefix(e.Name(), ".")
}
