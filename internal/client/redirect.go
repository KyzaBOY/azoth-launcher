package client

import (
	"encoding/binary"
	"fmt"
)

// Patches #3 and #4: in the code, the instruction that fetches a string from
// the game is replaced by a `lea rsi, [rip+X]` that points to a string of OURS,
// written in the .rdata slack (the page after VirtualSize, mapped and zeroed by
// the loader). Both spots were read in Ghidra (exe 1.400, 2026-10-20):
//
//   - #3, host of the ICMP pinger (FUN_146454580, slot 3 of vtable 0x1484fa5d8):
//     `cmp [r8+18h],10h; jb; mov rsi,[r8]` fetches the host the pinger builds
//     (dynamodb.<region>.amazonaws.com, FUN_1474b9990). With the patch the ICMP
//     goes straight to our server: no hosts file line and nothing listening on 443.
//   - #4, persona of the stubbed login (FUN_146438d70): persona = the auth
//     manager's (vtable+0x30; with sonic/none it is already the Windows user
//     name); empty -> the javelin.impersonate-persona-id property; empty ->
//     GetUserNameA (FUN_140875780), whose result `mov rsi,rax; cmp; jbe; mov
//     rsi,[rax]` fetches. The two `jne` that leave early (persona already
//     filled in) become nops, so the function always reaches the replaced spot
//     and copies our string over it. With the patch the persona becomes the
//     Discord login ticket. (Test round of 2026-10-20: with the lea alone the
//     persona stayed the Windows user name, because the auth manager already
//     returns the name.)

// RdataSlackStart / RdataSlackEnd delimit the slack in use: .rdata has
// VirtualSize 0x1fb151e starting at RVA 0x7ec9000 (ending at 0x9e7a51e) and
// .data starts at 0x9e7b000. From RVA 0x9e7a600 on it is not even in the file
// (end of the raw data), so it is surely zero.
const (
	RdataSlackStart uint64 = 0x9e7a600
	RdataSlackEnd   uint64 = 0x9e7b000
	// MaxRedirectLen is the cap of each string (without the NUL).
	MaxRedirectLen = 255
)

// StringRedirect is a code spot that starts reading a string of ours.
type StringRedirect struct {
	Name    string      // "ping-host", "persona" (goes to the log)
	SiteRVA uint64      // first replaced instruction
	Orig    []byte      // original bytes at the spot
	Tail    []byte      // nop that fills Orig up after the 7-byte lea
	SlotRVA uint64      // where the string lives, inside the .rdata slack
	Also    []CodePatch // extra code changes needed to reach the spot
}

// CodePatch replaces fixed bytes in the code.
type CodePatch struct {
	RVA       uint64
	Orig, New []byte
}

// nop6 is the 6-byte nop (66 0F 1F 44 00 00) that erases a jcc rel32.
var nop6 = []byte{0x66, 0x0F, 0x1F, 0x44, 0x00, 0x00}

var (
	// PingHostRedirect: 0x1464545a3 `49 83 78 18 10 | 72 03 | 49 8B 30`.
	PingHostRedirect = StringRedirect{
		Name:    "ping-host",
		SiteRVA: 0x64545a3,
		Orig:    []byte{0x49, 0x83, 0x78, 0x18, 0x10, 0x72, 0x03, 0x49, 0x8B, 0x30},
		Tail:    []byte{0x0F, 0x1F, 0x00},
		SlotRVA: RdataSlackStart,
	}
	// PersonaRedirect: 0x146438e1d `48 8B F0 | 48 83 78 18 0F | 76 09 | 48 8B 30`.
	PersonaRedirect = StringRedirect{
		Name:    "persona",
		SiteRVA: 0x6438e1d,
		Orig:    []byte{0x48, 0x8B, 0xF0, 0x48, 0x83, 0x78, 0x18, 0x0F, 0x76, 0x09, 0x48, 0x8B, 0x30},
		Tail:    nop6,
		SlotRVA: RdataSlackStart + 0x200,
		Also: []CodePatch{
			{RVA: 0x6438dd4, Orig: []byte{0x0F, 0x85, 0x0F, 0x01, 0x00, 0x00}, New: nop6}, // jne: the auth manager gave a name
			{RVA: 0x6438e0d, Orig: []byte{0x0F, 0x85, 0xD6, 0x00, 0x00, 0x00}, New: nop6}, // jne: the property gave a name
		},
	}
)

// Patched returns the new bytes of the spot: `lea rsi,[rip+disp32]` + Tail.
func (r StringRedirect) Patched() []byte {
	disp := int32(int64(r.SlotRVA) - int64(r.SiteRVA+7))
	b := []byte{0x48, 0x8D, 0x35, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[3:], uint32(disp))
	return append(b, r.Tail...)
}

// validRedirectValue: visible ASCII only, no spaces (a host or a ticket).
func validRedirectValue(v string) error {
	if v == "" || len(v) > MaxRedirectLen {
		return fmt.Errorf("value must have 1..%d bytes (got %d)", MaxRedirectLen, len(v))
	}
	for i := 0; i < len(v); i++ {
		if v[i] <= 0x20 || v[i] >= 0x7f {
			return fmt.Errorf("value has a forbidden byte 0x%02X at %d", v[i], i)
		}
	}
	return nil
}
