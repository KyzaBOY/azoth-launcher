package client

import (
	"bytes"
	"fmt"

	"golang.org/x/sys/windows"
)

// Patch #5: the region name in the menu. With -GatewayMode stubbed the client
// builds its only channel from a JSON literal in .rdata (read by the Channel
// Service data provider, next to the "LoginChannelOverride" string):
//
//	{ "channelName": "localhost", "localhost": { "displayName": "Local Host",
//	  "localizedName": "@mm_localhost", "publicApis": [ ... ] } }
//
// The menu shows "Local Host". We overwrite the two quoted values in place with
// our name, padded with spaces AFTER the closing quote (JSON whitespace), so the
// literal keeps its length and stays valid JSON. A localizedName without '@' is
// shown as is by the localization.

// RegionField is one quoted JSON value inside the literal.
type RegionField struct {
	Name string // "displayName", "localizedName" (goes to the log)
	RVA  uint64 // the opening quote of the value
	Orig string // the original value, quotes included
}

// RegionFields were read from exe 1.400 (file offset + 0x1800 = RVA in .rdata;
// each string appears once in the file).
var RegionFields = []RegionField{
	{Name: "displayName", RVA: 0x8501970, Orig: `"Local Host"`},
	{Name: "localizedName", RVA: 0x85019ae, Orig: `"@mm_localhost"`},
}

// MaxRegionNameLen fits the shorter field (`"Local Host"` = 10 + 2 quotes).
const MaxRegionNameLen = 10

// regionValue is the bytes that replace orig: `"name"` + spaces up to len(orig).
func regionValue(orig, name string) ([]byte, error) {
	if name == "" || len(name)+2 > len(orig) {
		return nil, fmt.Errorf("region name must have 1..%d bytes (got %d)", len(orig)-2, len(name))
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			return nil, fmt.Errorf("region name has a forbidden byte 0x%02X at %d", c, i)
		}
	}
	out := bytes.Repeat([]byte{' '}, len(orig))
	out[0] = '"'
	copy(out[1:], name)
	out[len(name)+1] = '"'
	return out, nil
}

// applyRegionName writes name over both fields. Each field is checked first:
// the original value is replaced, our value already there carries on, anything
// else aborts before writing that field.
func applyRegionName(proc windows.Handle, base uintptr, name string, logf func(string, ...any)) error {
	for _, f := range RegionFields {
		want, err := regionValue(f.Orig, name)
		if err != nil {
			return fmt.Errorf("[region] %w", err)
		}
		addr := base + uintptr(f.RVA)
		cur := make([]byte, len(f.Orig))
		if err := readMem(proc, addr, cur); err != nil {
			return fmt.Errorf("[region] read %s at 0x%X: %w", f.Name, addr, err)
		}
		switch {
		case bytes.Equal(cur, want):
			logf("[region] %s already %q: nothing to write", f.Name, name)
			continue
		case !bytes.Equal(cur, []byte(f.Orig)):
			return fmt.Errorf("[region] %w: %s = %q (expected %s)", errUnexpectedBytes, f.Name, cur, f.Orig)
		}
		if err := writeMem(proc, addr, want, windows.PAGE_READWRITE, false); err != nil {
			return fmt.Errorf("[region] in-memory patch of %s: %w", f.Name, err)
		}
		after := make([]byte, len(want))
		if err := readMem(proc, addr, after); err != nil {
			return fmt.Errorf("[region] re-read %s: %w", f.Name, err)
		}
		if !bytes.Equal(after, want) {
			return fmt.Errorf("[region] %w: %s = %q", errPatchNotConfirmed, f.Name, after)
		}
		logf("[region] %s: %s -> %q", f.Name, f.Orig, name)
	}
	return nil
}
