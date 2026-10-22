package client

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The Azoth title logo. At the end of the tutorial the game shows its own title
// logo, the texture LogoEntry inside DataStrm-part3.pak. The launcher puts the
// same path, with our artwork, in a small pak of its own.
//
// The game mounts every <install>\assets\*.pak (the "@assets@/*.pak" mount in
// the exe; PaksList.lst is only used to check that the listed paks open), so
// the logo pak has to sit in the install's assets folder. It is the only file
// the launcher ever adds there, and no game file is changed. The same pak goes
// in under two names, one sorted before every game pak and one after them, so
// it wins whichever way the engine resolves a file that is in two paks.
//
// internal/client/logo/outrologo.dds is built from outrologo.png by
// scripts/make-logo.py (same format as the game's texture: BC3, DX10 header).

// LogoEntry is the path of the logo texture inside the game's paks.
const LogoEntry = "lyshineui/images/hud/tutorialmessage/outrologo.dds"

// LogoPakNames are the file names of the logo pak in <install>\assets.
var LogoPakNames = []string{"00_azoth_logo.pak", "zz_azoth_logo.pak"}

//go:embed logo/outrologo.dds
var logoDDS []byte

// LogoPak returns the logo pak: a zip with LogoEntry stored uncompressed and a
// fixed timestamp, so the bytes are the same on every run.
func LogoPak() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:     LogoEntry,
		Method:   zip.Store,
		Modified: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(logoDDS); err != nil {
		return nil, err
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
