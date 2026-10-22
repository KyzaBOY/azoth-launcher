package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrSteamInstall is returned when the folder to run from is an install of the
// game (Steam or otherwise): a project rule, we never run or patch inside it.
var ErrSteamInstall = errors.New("this is the game's install folder; Azoth only runs on an isolated copy (it never touches the install)")

// ErrUnsupportedBuild is returned when the sha256 does not match the supported build.
var ErrUnsupportedBuild = errors.New("unsupported NewWorld.exe build")

// ErrTooShort is returned by NormalizedSHA256 for a file that is too small to
// be NewWorld.exe (it ends before the patch sites).
var ErrTooShort = errors.New("file too short to be NewWorld.exe")

// rootFiles are the files the isolated copy needs at its root (next to
// Bin64), besides the assets junction.
var rootFiles = []string{"bootstrap.cfg", "engine.json", "system_windows_pc.cfg", "PaksList.lst", "typeindex.json"}

// patchSite is a spot of the file that may be patched on disk and that the
// hash normalizes back to the original.
type patchSite struct {
	Name    string
	Offset  int64
	Orig    []byte
	Patched []byte
}

// PatchSites are the two patches that can live in the file, in offset order.
var PatchSites = []patchSite{
	{Name: "dtls", Offset: PatchFileOffset, Orig: OrigBytes[:], Patched: PatchedBytes[:]},
	{Name: "ping", Offset: PingPatchFileOffset, Orig: PingOrigBytes[:], Patched: PingPatchedBytes[:]},
}

// IsSteamInstall says whether binDir is inside an install of the game. It
// looks at the path (steamapps\common or steamapps/common) and at the marks
// only an install has.
func IsSteamInstall(binDir string) bool {
	low := strings.ToLower(strings.ReplaceAll(filepath.Clean(binDir), `\`, "/"))
	if strings.Contains(low, "/steamapps/common/") {
		return true
	}
	root := filepath.Dir(binDir)
	for _, mark := range []string{"installscript.vdf", "NewWorldLauncher.exe"} {
		if _, err := os.Stat(filepath.Join(root, mark)); err == nil {
			return true
		}
	}
	if st, err := os.Stat(filepath.Join(root, "EasyAntiCheat")); err == nil && st.IsDir() {
		return true
	}
	return false
}

// NormalizedSHA256 computes the sha256 of the exe treating every PatchSites
// spot as the original bytes when it is patched. It also returns which spots
// were patched on disk. Any other content at those spots goes into the hash as
// it is (and the hash will not match).
func NormalizedSHA256(path string) (sum string, patched map[string]bool, err error) {
	return normalizedSHA256(path, PatchSites)
}

// normalizedSHA256 is NormalizedSHA256 with the spots as a parameter (to test
// without a 179 MB file).
func normalizedSHA256(path string, sites []patchSite) (string, map[string]bool, error) {
	sites = append([]patchSite(nil), sites...)
	sort.Slice(sites, func(i, j int) bool { return sites[i].Offset < sites[j].Offset })

	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	size := st.Size()

	patched := map[string]bool{}
	h := sha256.New()
	var pos int64
	for _, s := range sites {
		if s.Offset < pos {
			return "", nil, fmt.Errorf("overlapping patch sites (%s)", s.Name)
		}
		if size < s.Offset+int64(len(s.Orig)) {
			return "", nil, fmt.Errorf("%w (%d bytes)", ErrTooShort, size)
		}
		if _, err := io.Copy(h, io.NewSectionReader(f, pos, s.Offset-pos)); err != nil {
			return "", nil, err
		}
		window := make([]byte, len(s.Orig))
		if _, err := f.ReadAt(window, s.Offset); err != nil {
			return "", nil, err
		}
		if bytes.Equal(window, s.Patched) {
			patched[s.Name] = true
			window = s.Orig
		}
		h.Write(window)
		pos = s.Offset + int64(len(s.Orig))
	}
	if _, err := io.Copy(h, io.NewSectionReader(f, pos, size-pos)); err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(h.Sum(nil)), patched, nil
}
