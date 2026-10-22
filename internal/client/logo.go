package client

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The Azoth logos. The game shows its own title logo on the title screen
// ("press any button") and at the end of the tutorial, two textures inside
// DataStrm-part3.pak (LogoEntries). The launcher puts the same paths, with our
// artwork, in a small pak of its own.
//
// The game mounts every <install>\assets\*.pak (the "@assets@/*.pak" mount in
// the exe; PaksList.lst is only used to check that the listed paks open), so
// the logo pak has to sit in the install's assets folder. It is the only file
// the launcher ever adds there, and no game file is changed. The same pak goes
// in under two names, one sorted before every game pak and one after them, so
// it wins whichever way the engine resolves a file that is in two paks.
//
// The .dds files in internal/client/logo are built from the .png next to them
// by scripts/make-logo.py (BC3 in a DX10 header, like the game's UI textures).

// LogoEntries maps each texture path inside the game's paks to our version.
var LogoEntries = map[string][]byte{
	"lyshineui/images/frontend/newworldlogo.dds":         titleLogoDDS, // title screen
	"lyshineui/images/hud/tutorialmessage/outrologo.dds": outroLogoDDS, // end of the tutorial
}

// LogoPakNames are the file names of the logo pak in <install>\assets.
var LogoPakNames = []string{"00_azoth_logo.pak", "zz_azoth_logo.pak"}

var (
	//go:embed logo/titlelogo.dds
	titleLogoDDS []byte
	//go:embed logo/outrologo.dds
	outroLogoDDS []byte
)

// LogoPak returns the logo pak: a zip with every LogoEntries file stored
// uncompressed, in name order and with a fixed timestamp, so the bytes are the
// same on every run.
func LogoPak() ([]byte, error) {
	names := make([]string, 0, len(LogoEntries))
	for name := range LogoEntries {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:     name,
			Method:   zip.Store,
			Modified: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(LogoEntries[name]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// InstallLogo writes the logo pak into assetsDir (the install's assets folder)
// under each of LogoPakNames. A file that already holds exactly these bytes is
// left alone; nothing else in the folder is touched.
func InstallLogo(assetsDir string) error {
	if st, err := os.Stat(assetsDir); err != nil || !st.IsDir() {
		return fmt.Errorf("assets folder not found: %s", assetsDir)
	}
	pak, err := LogoPak()
	if err != nil {
		return err
	}
	for _, name := range LogoPakNames {
		dst := filepath.Join(assetsDir, name)
		if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, pak) {
			continue
		}
		tmp := dst + ".tmp"
		if err := os.WriteFile(tmp, pak, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}
