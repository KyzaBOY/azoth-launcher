package client

import "errors"

// LaunchOptions describes a launch of the client.
type LaunchOptions struct {
	BinDir      string       // Bin64 folder of the isolated copy (becomes the game's cwd)
	GatewayAddr string       // host:port of the gateway
	PingHost    string       // patch #3: host the ICMP pinger measures ("" = leave the original one)
	Persona     string       // patch #4: persona of the stubbed login (the Discord ticket); "" = Windows user name
	RegionName  string       // patch #5: region name in the menu instead of "Local Host"; "" = leave it
	Log         func(string) // gets every step as text (optional)
}

// LaunchResult reports what happened to the process.
type LaunchResult struct {
	PID         uint32
	CommandLine string
	ImageBase   string // hex

	// patch #1 (DTLS)
	PatchAddress string // hex
	BytesBefore  string // "0F 84 19 01 00 00"
	BytesAfter   string // "E9 1A 01 00 00 90"
	PatchedInMem bool   // true = the launcher wrote it; false = it was already patched on disk

	// patch #2 (ICMP pinger)
	PingAddress      string // hex
	PingBefore       string // pointer read, hex
	PingAfter        string // pointer afterwards, hex
	PingPatchedInMem bool   // true = the launcher wrote it

	PingHost       string // patch #3: the host the ICMP pinger measures
	PersonaPatched bool   // patch #4: persona = ticket (the value is never part of the report)
	RegionName     string // patch #5: the region name the menu shows
}

// ErrPatchFailed wraps every error that made Launch kill the suspended process
// before the game ran a single instruction (a patch could not be applied or
// confirmed, or the process could not be inspected or resumed). The game was
// not started.
var ErrPatchFailed = errors.New("could not patch the game process")
