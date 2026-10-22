package client

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLogoDDSFormat(t *testing.T) {
	d := logoDDS
	if len(d) != 148+800*220 || string(d[:4]) != "DDS " {
		t.Fatalf("logo dds: %d bytes, magic %q", len(d), d[:4])
	}
	h, w := binary.LittleEndian.Uint32(d[12:]), binary.LittleEndian.Uint32(d[16:])
	if w != 800 || h != 220 || string(d[84:88]) != "DX10" {
		t.Fatalf("logo dds: %dx%d, fourcc %q (want 800x220 DX10)", w, h, d[84:88])
	}
	if f := binary.LittleEndian.Uint32(d[128:]); f != 78 { // DXGI_FORMAT_BC3_UNORM_SRGB, as the game's texture
		t.Fatalf("logo dds: DXGI format %d, want 78", f)
	}
}

func TestLogoPak(t *testing.T) {
	pak, err := LogoPak()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := LogoPak()
	if !bytes.Equal(pak, again) {
		t.Fatal("LogoPak is not deterministic")
	}
	zr, err := zip.NewReader(bytes.NewReader(pak), int64(len(pak)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != LogoEntry || zr.File[0].Method != zip.Store {
		t.Fatalf("pak entries: %+v", zr.File)
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc) // also checks the CRC
	if err != nil || !bytes.Equal(got, logoDDS) {
		t.Fatalf("pak content differs from the embedded dds (err %v)", err)
	}
}

func TestInstallLogo(t *testing.T) {
	assets := t.TempDir()
	other := filepath.Join(assets, "DataStrm-part3.pak")
	if err := os.WriteFile(other, []byte("game pak"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a stale logo pak from an older launcher gets replaced
	if err := os.WriteFile(filepath.Join(assets, LogoPakNames[0]), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallLogo(assets); err != nil {
		t.Fatal(err)
	}
	pak, _ := LogoPak()
	for _, name := range LogoPakNames {
		got, err := os.ReadFile(filepath.Join(assets, name))
		if err != nil || !bytes.Equal(got, pak) {
			t.Fatalf("%s not written (err %v)", name, err)
		}
	}
	if got, _ := os.ReadFile(other); string(got) != "game pak" {
		t.Fatal("a game file was touched")
	}
	st0, _ := os.Stat(filepath.Join(assets, LogoPakNames[1]))
	if err := InstallLogo(assets); err != nil { // second run: nothing to do
		t.Fatal(err)
	}
	st1, _ := os.Stat(filepath.Join(assets, LogoPakNames[1]))
	if !st0.ModTime().Equal(st1.ModTime()) {
		t.Fatal("an up-to-date logo pak was rewritten")
	}
	entries, _ := os.ReadDir(assets)
	if len(entries) != 1+len(LogoPakNames) {
		t.Fatalf("assets has %d files, want %d (leftover .tmp?)", len(entries), 1+len(LogoPakNames))
	}
	if err := InstallLogo(filepath.Join(assets, "missing")); err == nil {
		t.Fatal("InstallLogo accepted a missing folder")
	}
}
