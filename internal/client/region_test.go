package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The literal as it sits in exe 1.400 (the span with its real padding, the rest collapsed).
var stubChannelJSON = `{ "channelName": "localhost", "localhost": { "displayName": ` + RegionFields[0].Orig +
	`"publicApis": [ { "awsRegion": "us-east-1", "version": "1.0", "apiEndpoint": "127.0.0.1", "clientTag": "authStack" } ] } }`

func TestRegionValueKeepsJSON(t *testing.T) {
	for _, name := range []string{"NA", "Azoth Bug Hunting", strings.Repeat("x", MaxRegionNameLen)} {
		checkRegionJSON(t, name)
	}
}

func checkRegionJSON(t *testing.T, name string) {
	t.Helper()
	s := stubChannelJSON
	for _, f := range RegionFields {
		v, err := regionValue(f.Orig, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != len(f.Orig) {
			t.Fatalf("%s: %d bytes, orig %d", f.Name, len(v), len(f.Orig))
		}
		if !strings.Contains(s, f.Orig) {
			t.Fatalf("%s: %s not in the literal", f.Name, f.Orig)
		}
		s = strings.Replace(s, f.Orig, string(v), 1)
	}
	if len(s) != len(stubChannelJSON) {
		t.Fatalf("length changed: %d -> %d", len(stubChannelJSON), len(s))
	}
	var doc struct {
		Localhost struct {
			DisplayName   string `json:"displayName"`
			LocalizedName string `json:"localizedName"`
		} `json:"localhost"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("patched literal is not JSON: %v\n%s", err, s)
	}
	if doc.Localhost.DisplayName != name || doc.Localhost.LocalizedName != name {
		t.Fatalf("got %+v", doc.Localhost)
	}
}

func TestRegionValueRejects(t *testing.T) {
	orig := RegionFields[0].Orig
	for _, bad := range []string{"", strings.Repeat("x", MaxRegionNameLen+1), `a"b`, `a\b`, "x\x00", "jão"} {
		if _, err := regionValue(orig, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := regionValue(orig, strings.Repeat("x", MaxRegionNameLen)); err != nil {
		t.Errorf("max length refused: %v", err)
	}
}

// TestRegionFieldsInExe checks the original values in the real exe when
// AZOTH_CLIENT_BIN points at a Bin64 folder (the exe never goes in the repo).
func TestRegionFieldsInExe(t *testing.T) {
	bin := os.Getenv("AZOTH_CLIENT_BIN")
	if bin == "" {
		t.Skip("AZOTH_CLIENT_BIN not set")
	}
	f, err := os.Open(filepath.Join(bin, ExeName))
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	for _, r := range RegionFields {
		b := make([]byte, len(r.Orig))
		if _, err := f.ReadAt(b, int64(r.RVA)-0x1800); err != nil { // .rdata: offset = RVA - 0x1800
			t.Fatal(err)
		}
		if string(b) != r.Orig {
			t.Fatalf("%s: exe has %q, want %s", r.Name, b, r.Orig)
		}
	}
}
