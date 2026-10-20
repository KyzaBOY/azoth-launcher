package client

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestRedirectEncoding(t *testing.T) {
	for _, r := range []StringRedirect{PingHostRedirect, PersonaRedirect} {
		p := r.Patched()
		if len(p) != len(r.Orig) {
			t.Fatalf("%s: patched %d bytes, orig %d", r.Name, len(p), len(r.Orig))
		}
		if !bytes.Equal(p[:3], []byte{0x48, 0x8D, 0x35}) {
			t.Fatalf("%s: not lea rsi,[rip+x]: %s", r.Name, Hex(p))
		}
		disp := int64(int32(binary.LittleEndian.Uint32(p[3:7])))
		if got := int64(r.SiteRVA) + 7 + disp; uint64(got) != r.SlotRVA {
			t.Fatalf("%s: lea aims at RVA 0x%X, want 0x%X", r.Name, got, r.SlotRVA)
		}
		if r.SlotRVA < RdataSlackStart || r.SlotRVA+MaxRedirectLen+1 > RdataSlackEnd {
			t.Fatalf("%s: slot outside the slack", r.Name)
		}
	}
	if PingHostRedirect.SlotRVA+MaxRedirectLen+1 > PersonaRedirect.SlotRVA {
		t.Fatal("slots overlap")
	}
}

func TestRedirectValue(t *testing.T) {
	for _, ok := range []string{"play.example.test", "projectazoth.com", "az1-AbC_-09"} {
		if err := validRedirectValue(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "x\x00", string(make([]byte, 256))} {
		if validRedirectValue(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// TestRedirectSitesInExe checks the original bytes in the real exe when
// AZOTH_CLIENT_BIN points at a Bin64 folder (the exe never goes in the repo).
func TestRedirectSitesInExe(t *testing.T) {
	bin := os.Getenv("AZOTH_CLIENT_BIN")
	if bin == "" {
		t.Skip("AZOTH_CLIENT_BIN not set")
	}
	f, err := os.Open(filepath.Join(bin, ExeName))
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	for _, r := range []StringRedirect{PingHostRedirect, PersonaRedirect} {
		b := make([]byte, len(r.Orig))
		if _, err := f.ReadAt(b, int64(r.SiteRVA)-0xA00); err != nil { // .text: offset = RVA - 0xA00
			t.Fatal(err)
		}
		if !bytes.Equal(b, r.Orig) {
			t.Fatalf("%s: exe has %s, want %s", r.Name, Hex(b), Hex(r.Orig))
		}
		for _, c := range r.Also {
			b := make([]byte, len(c.Orig))
			if _, err := f.ReadAt(b, int64(c.RVA)-0xA00); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b, c.Orig) || len(c.New) != len(c.Orig) {
				t.Fatalf("%s: exe has %s at 0x%X, want %s", r.Name, Hex(b), c.RVA, Hex(c.Orig))
			}
		}
	}
}
