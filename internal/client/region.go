package client

import (
	"bytes"
	"fmt"
	"strings"
)

// Patch #5: the region name in the menu. With -GatewayMode stubbed the client
// builds its only channel from a JSON literal in .rdata (read by the Channel
// Service data provider, next to the "LoginChannelOverride" string):
//
//	{ "channelName": "localhost", "localhost": { "displayName": "Local Host",
//	  "localizedName": "@mm_localhost", "publicApis": [ ... ] } }
//
// The menu shows "Local Host". The literal is padded with runs of spaces (JSON
// whitespace), so we rewrite one span in place: from the opening quote of the
// displayName value up to "publicApis", both values with our name, and the
// rest of the span as spaces. The literal keeps its length and stays valid
// JSON, and the name can be up to MaxRegionNameLen bytes. A localizedName
// without '@' is shown as is by the localization.

// RegionField is one span inside the literal.
type RegionField struct {
	Name string // goes to the log
	RVA  uint64 // the first byte of the span
	Orig string // the original bytes of the span
}

// RegionFields were read from exe 1.400 (file offset + 0x1800 = RVA in .rdata;
// each string appears once in the file).
var RegionFields = []RegionField{{
	Name: "displayName+localizedName",
	RVA:  0x8501970,
	Orig: `"Local Host",` + strings.Repeat(" ", 32) + `"localizedName": "@mm_localhost",` + strings.Repeat(" ", 27),
}}

// regionTemplate is what the span becomes, before the padding.
const regionTemplate = `"%s", "localizedName": "%s",`

// MaxRegionNameLen fits the span (105 bytes) with both values.
const MaxRegionNameLen = 40

// regionValue is the bytes that replace orig: both values with name, then spaces up to len(orig).
func regionValue(orig, name string) ([]byte, error) {
	if name == "" || len(name) > MaxRegionNameLen || len(fmt.Sprintf(regionTemplate, name, name)) > len(orig) {
		return nil, fmt.Errorf("region name must have 1..%d bytes (got %d)", MaxRegionNameLen, len(name))
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c >= 0x7f || c == '"' || c == '\\' {
			return nil, fmt.Errorf("region name has a forbidden byte 0x%02X at %d", c, i)
		}
	}
	out := bytes.Repeat([]byte{' '}, len(orig))
	copy(out, fmt.Sprintf(regionTemplate, name, name))
	return out, nil
}
