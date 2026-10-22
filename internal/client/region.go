package client

import (
	"bytes"
	"fmt"
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
