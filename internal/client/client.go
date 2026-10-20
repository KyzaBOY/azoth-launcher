// Package client knows everything about the NewWorld.exe the launcher runs:
// where it lives (always an isolated copy, never the Steam install), whether it
// is the supported build (sha256) and how to launch it with the patches applied
// IN MEMORY, so no game file is ever changed.
//
// The numbers below come from reverse engineering build 1.400.6031.6004151 and
// were checked against the PE headers of the exe:
//
//	.text  VA 0x1000     raw 0x600      -> RVA = file offset + 0xA00
//	.rdata VA 0x7ec9000  raw 0x7ec7800  -> RVA = file offset + 0x1800
//	preferred image base: 0x140000000
package client

import (
	"fmt"
	"strings"
)

const (
	// ExpectedSHA256 is the sha256 of the NewWorld.exe of the only supported
	// build, measured on the ORIGINAL file of the install (no patch at all).
	ExpectedSHA256 = "8654f01d324636d9f74f1c793b0cc4a417c3c5fa9847d9913c358ca29e0fdc8e"

	// ---- patch #1: DTLS trust ----------------------------------------------
	// `je 0x5dce9d4` inside SecureSocketDriver::Initialize: the jump that
	// decides between validating the world server's certificate and accepting
	// any of them. It becomes `jmp` + `nop`.

	// PatchFileOffset is the FILE offset of the je.
	PatchFileOffset int64 = 0x5dcdeb5
	// PatchRVA is the same spot as an RVA (0x5dcdeb5 + 0xA00).
	PatchRVA uint64 = 0x5dce8b5
	// PatchLen is the size of the replaced instruction (je rel32 = 6 bytes).
	PatchLen = 6

	// ---- patch #2: the menu's region pinger over ICMP ----------------------
	// An 8-byte entry in the pattern table in .rdata, read only by registry
	// 0x644c730: an absolute pointer to the string "https". The "icmp" string
	// sits 16 bytes before it. With https the menu ping reads 9999 ms (the
	// client rejects the gateway's self-signed certificate); with ICMP, and
	// patch #3 pointing it at our host, it measures our server.

	// PingPatchRVA is the RVA of the table entry.
	PingPatchRVA uint64 = 0x84f9e30
	// PingPatchFileOffset is the same spot as a file offset (RVA - 0x1800).
	PingPatchFileOffset int64 = 0x84f8630
	// PingPatchLen is the size of the pointer (u64).
	PingPatchLen = 8
	// PingHTTPSRVA / PingICMPRVA are the "https" and "icmp" strings in .rdata.
	PingHTTPSRVA uint64 = 0x7fc4b50
	PingICMPRVA  uint64 = 0x7fc4b40
	// PreferredImageBase is the exe's preferred base (the PE ImageBase). Before
	// relocation the table pointer is PreferredImageBase + PingHTTPSRVA.
	PreferredImageBase uint64 = 0x140000000

	// ExeName is the client's executable name inside Bin64.
	ExeName = "NewWorld.exe"
)

var (
	// OrigBytes is the original `je` (0F 84 19 01 00 00): the path that validates the cert.
	OrigBytes = [PatchLen]byte{0x0F, 0x84, 0x19, 0x01, 0x00, 0x00}
	// PatchedBytes is `jmp 0x5dce9d4; nop` (E9 1A 01 00 00 90): always the
	// permissive path, which accepts the world server's self-signed cert.
	PatchedBytes = [PatchLen]byte{0xE9, 0x1A, 0x01, 0x00, 0x00, 0x90}

	// PingOrigBytes is the pointer 0x147fc4b50 ("https") as it sits in the FILE
	// (not relocated, little-endian).
	PingOrigBytes = [PingPatchLen]byte{0x50, 0x4B, 0xFC, 0x47, 0x01, 0x00, 0x00, 0x00}
	// PingPatchedBytes is the pointer 0x147fc4b40 ("icmp"), same way.
	PingPatchedBytes = [PingPatchLen]byte{0x40, 0x4B, 0xFC, 0x47, 0x01, 0x00, 0x00, 0x00}
)

// Hex formats bytes as "0F 84 19 01 00 00".
func Hex(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, " ")
}

// GameArgs builds the client's command line for the "stubbed" dev route: no
// Steam, no EAC, talking to our gateway.
func GameArgs(gatewayAddr string) []string {
	return []string{
		"-noprompt",
		"-ProductId", "dev",
		"-AuthManager", "sonic",
		"-AuthMode", "none",
		"-GatewayMode", "stubbed",
		"-GatewayAddr", gatewayAddr,
	}
}
