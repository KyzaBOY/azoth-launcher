package client

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// LaunchOptions describes a launch of the client.
type LaunchOptions struct {
	BinDir      string       // Bin64 folder of the isolated copy (becomes the game's cwd)
	GatewayAddr string       // host:port of the gateway
	PingHost    string       // patch #3: host the ICMP pinger measures ("" = leave the original one)
	Persona     string       // patch #4: persona of the stubbed login (the Discord ticket); "" = Windows user name
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
}

// ErrPatchFailed wraps every error that made Launch kill the suspended process
// before the game ran a single instruction (a patch could not be applied or
// confirmed, or the process could not be inspected or resumed). The game was
// not started.
var ErrPatchFailed = errors.New("could not patch the game process")

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procFlushInstrCache   = kernel32.NewProc("FlushInstructionCache")
	errUnexpectedBytes    = errors.New("unexpected bytes at the patch site (different build?)")
	errPatchNotConfirmed  = errors.New("the re-read did not confirm the in-memory patch")
	pebImageBaseOffset    = uintptr(0x10) // PEB64.ImageBaseAddress
	suspendedProcessFlags = uint32(windows.CREATE_SUSPENDED)
)

// Launch creates NewWorld.exe suspended, checks the image base through the
// PEB, applies (or confirms) the patches in memory and only then resumes the
// process. The process never runs a single game instruction before the patches
// are confirmed.
func Launch(opts LaunchOptions) (*LaunchResult, error) {
	logf := func(format string, a ...any) {
		if opts.Log != nil {
			opts.Log(fmt.Sprintf(format, a...))
		}
	}

	binDir, err := filepath.Abs(opts.BinDir)
	if err != nil {
		return nil, err
	}
	if IsSteamInstall(binDir) {
		return nil, ErrSteamInstall
	}
	exe := filepath.Join(binDir, ExeName)
	if _, err := os.Stat(exe); err != nil {
		return nil, fmt.Errorf("%s not found in %s", ExeName, binDir)
	}

	args := append([]string{exe}, GameArgs(opts.GatewayAddr)...)
	cmdLine := windows.ComposeCommandLine(args)
	res := &LaunchResult{CommandLine: cmdLine}

	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	cmdBuf, err := windows.UTF16FromString(cmdLine) // CreateProcessW may write to this buffer
	if err != nil {
		return nil, err
	}
	cwdPtr, err := windows.UTF16PtrFromString(binDir)
	if err != nil {
		return nil, err
	}

	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation

	logf("CreateProcess (CREATE_SUSPENDED) cwd=%s", binDir)
	logf("command line: %s", cmdLine)
	if err := windows.CreateProcess(exePtr, &cmdBuf[0], nil, nil, false, suspendedProcessFlags, nil, cwdPtr, &si, &pi); err != nil {
		return nil, fmt.Errorf("CreateProcess: %w", err)
	}
	defer windows.CloseHandle(pi.Thread)
	defer windows.CloseHandle(pi.Process)
	res.PID = pi.ProcessId
	logf("process created suspended, PID %d", pi.ProcessId)

	// Any error from here on kills the process: it must not run without the patches.
	kill := func(cause error) (*LaunchResult, error) {
		_ = windows.TerminateProcess(pi.Process, 1)
		_, _ = windows.WaitForSingleObject(pi.Process, 5000)
		logf("process %d terminated (TerminateProcess)", pi.ProcessId)
		return res, fmt.Errorf("%w: %w", ErrPatchFailed, cause)
	}

	base, err := imageBase(pi.Process)
	if err != nil {
		return kill(fmt.Errorf("image base: %w", err))
	}
	res.ImageBase = fmt.Sprintf("0x%X", base)
	logf("PEB.ImageBaseAddress = 0x%X", base)

	var mz [2]byte
	if err := readMem(pi.Process, base, mz[:]); err != nil {
		return kill(fmt.Errorf("read header at base: %w", err))
	}
	if mz != [2]byte{'M', 'Z'} {
		return kill(fmt.Errorf("base 0x%X has no MZ signature (%s)", base, Hex(mz[:])))
	}

	// ---- patch #1: DTLS (.text) -------------------------------------------
	addr := base + uintptr(PatchRVA)
	res.PatchAddress = fmt.Sprintf("0x%X", addr)
	var cur [PatchLen]byte
	if err := readMem(pi.Process, addr, cur[:]); err != nil {
		return kill(fmt.Errorf("read bytes at 0x%X: %w", addr, err))
	}
	res.BytesBefore = Hex(cur[:])
	logf("[dtls] bytes at base+0x%X (0x%X): %s", PatchRVA, addr, res.BytesBefore)

	switch cur {
	case PatchedBytes:
		logf("[dtls] already jmp+nop (copy patched on disk): nothing to write")
		res.BytesAfter = res.BytesBefore
	case OrigBytes:
		logf("[dtls] original je: applying in-memory patch -> %s", Hex(PatchedBytes[:]))
		if err := writeMem(pi.Process, addr, PatchedBytes[:], windows.PAGE_EXECUTE_READWRITE, true); err != nil {
			return kill(fmt.Errorf("[dtls] in-memory patch: %w", err))
		}
		var after [PatchLen]byte
		if err := readMem(pi.Process, addr, after[:]); err != nil {
			return kill(fmt.Errorf("[dtls] re-read patch: %w", err))
		}
		res.BytesAfter = Hex(after[:])
		if after != PatchedBytes {
			return kill(fmt.Errorf("[dtls] %w: read %s", errPatchNotConfirmed, res.BytesAfter))
		}
		res.PatchedInMem = true
		logf("[dtls] re-read confirmed: %s", res.BytesAfter)
	default:
		return kill(fmt.Errorf("[dtls] %w: %s (expected %s or %s)", errUnexpectedBytes, res.BytesBefore, Hex(OrigBytes[:]), Hex(PatchedBytes[:])))
	}

	// ---- patch #2: region pinger over ICMP (.rdata) ------------------------
	if err := applyPingPatch(pi.Process, base, res, logf); err != nil {
		return kill(err)
	}
	// ---- patch #3: the host the pinger measures ----------------------------
	if opts.PingHost != "" {
		if err := applyRedirect(pi.Process, base, PingHostRedirect, opts.PingHost, logf); err != nil {
			return kill(err)
		}
		res.PingHost = opts.PingHost
		logf("[ping-host] the menu ping measures %s (no hosts file, no proxy)", opts.PingHost)
	}

	// ---- patch #4: persona = the Discord login ticket ----------------------
	if opts.Persona != "" {
		if err := applyRedirect(pi.Process, base, PersonaRedirect, opts.Persona, logf); err != nil {
			return kill(err)
		}
		res.PersonaPatched = true
	} else {
		logf("[persona] not logged in: the client uses the Windows user name as persona")
	}

	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		return kill(fmt.Errorf("ResumeThread: %w", err))
	}
	logf("ResumeThread: the game is running (PID %d)", pi.ProcessId)

	// If the client dies in the first moments, say so (missing DLL, etc.).
	if ev, _ := windows.WaitForSingleObject(pi.Process, uint32((1500 * time.Millisecond).Milliseconds())); ev == windows.WAIT_OBJECT_0 {
		var code uint32
		_ = windows.GetExitCodeProcess(pi.Process, &code)
		return res, fmt.Errorf("the client exited right after starting (exit code %d / 0x%X)", code, code)
	}
	return res, nil
}

// applyPingPatch swaps, in the pattern table in .rdata, the pointer to "https"
// for the pointer to "icmp" (16 bytes before it). It is robust to relocation:
// it accepts the already relocated value (base + RVA) or the value in the file
// (preferred base + RVA) and writes `value - 0x10`. Already "icmp" = a copy
// patched on disk, carries on. Any other value aborts without writing anything.
func applyPingPatch(proc windows.Handle, base uintptr, res *LaunchResult, logf func(string, ...any)) error {
	addr := base + uintptr(PingPatchRVA)
	res.PingAddress = fmt.Sprintf("0x%X", addr)
	var raw [PingPatchLen]byte
	if err := readMem(proc, addr, raw[:]); err != nil {
		return fmt.Errorf("[ping] read pointer at 0x%X: %w", addr, err)
	}
	cur := binary.LittleEndian.Uint64(raw[:])
	res.PingBefore = fmt.Sprintf("0x%X", cur)
	logf("[ping] pointer at base+0x%X (0x%X): %s (%s)", PingPatchRVA, addr, res.PingBefore, Hex(raw[:]))

	httpsReloc := uint64(base) + PingHTTPSRVA
	icmpReloc := uint64(base) + PingICMPRVA
	httpsPref := PreferredImageBase + PingHTTPSRVA
	icmpPref := PreferredImageBase + PingICMPRVA

	switch cur {
	case icmpReloc, icmpPref:
		logf("[ping] already points to \"icmp\" (copy patched on disk): nothing to write")
		res.PingAfter = res.PingBefore
		return nil
	case httpsReloc, httpsPref:
		want := cur - 0x10
		state := "already relocated (base + RVA)"
		if cur == httpsPref && httpsPref != httpsReloc {
			state = "not yet relocated (preferred base 0x140000000)"
		}
		var out [PingPatchLen]byte
		binary.LittleEndian.PutUint64(out[:], want)
		logf("[ping] points to \"https\", %s: writing 0x%X (\"icmp\")", state, want)
		if err := writeMem(proc, addr, out[:], windows.PAGE_READWRITE, false); err != nil {
			return fmt.Errorf("[ping] in-memory patch: %w", err)
		}
		var after [PingPatchLen]byte
		if err := readMem(proc, addr, after[:]); err != nil {
			return fmt.Errorf("[ping] re-read patch: %w", err)
		}
		got := binary.LittleEndian.Uint64(after[:])
		res.PingAfter = fmt.Sprintf("0x%X", got)
		if got != want {
			return fmt.Errorf("[ping] %w: read %s", errPatchNotConfirmed, res.PingAfter)
		}
		res.PingPatchedInMem = true
		logf("[ping] re-read confirmed: %s", res.PingAfter)
		return nil
	default:
		return fmt.Errorf("[ping] %w: %s (expected 0x%X/0x%X = https or 0x%X/0x%X = icmp)", errUnexpectedBytes, res.PingBefore, httpsReloc, httpsPref, icmpReloc, icmpPref)
	}
}

// imageBase reads the process PEB (NtQueryInformationProcess ->
// ProcessBasicInformation) and returns PEB64.ImageBaseAddress (PEB+0x10).
func imageBase(proc windows.Handle) (uintptr, error) {
	var pbi windows.PROCESS_BASIC_INFORMATION
	var retLen uint32
	if err := windows.NtQueryInformationProcess(proc, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), &retLen); err != nil {
		return 0, fmt.Errorf("NtQueryInformationProcess: %w", err)
	}
	peb := uintptr(unsafe.Pointer(pbi.PebBaseAddress)) // address in the OTHER process: never dereference it here
	if peb == 0 {
		return 0, errors.New("PebBaseAddress is null")
	}
	var raw [8]byte
	if err := readMem(proc, peb+pebImageBaseOffset, raw[:]); err != nil {
		return 0, fmt.Errorf("read PEB+0x10: %w", err)
	}
	base := uintptr(binary.LittleEndian.Uint64(raw[:]))
	if base == 0 {
		return 0, errors.New("ImageBaseAddress is null")
	}
	return base, nil
}

func readMem(proc windows.Handle, addr uintptr, buf []byte) error {
	var n uintptr
	if err := windows.ReadProcessMemory(proc, addr, &buf[0], uintptr(len(buf)), &n); err != nil {
		return fmt.Errorf("ReadProcessMemory(0x%X): %w", addr, err)
	}
	if n != uintptr(len(buf)) {
		return fmt.Errorf("ReadProcessMemory(0x%X): read %d of %d bytes", addr, n, len(buf))
	}
	return nil
}

// writeMem makes the page writable (newProt), writes, restores the original
// protection and, if it is code, flushes the region instruction cache.
func writeMem(proc windows.Handle, addr uintptr, data []byte, newProt uint32, flushCode bool) error {
	var oldProt uint32
	if err := windows.VirtualProtectEx(proc, addr, uintptr(len(data)), newProt, &oldProt); err != nil {
		return fmt.Errorf("VirtualProtectEx(0x%X): %w", newProt, err)
	}
	var n uintptr
	werr := windows.WriteProcessMemory(proc, addr, &data[0], uintptr(len(data)), &n)
	var dummy uint32
	if err := windows.VirtualProtectEx(proc, addr, uintptr(len(data)), oldProt, &dummy); err != nil && werr == nil {
		werr = fmt.Errorf("VirtualProtectEx(restore 0x%X): %w", oldProt, err)
	}
	if werr != nil {
		return fmt.Errorf("WriteProcessMemory: %w", werr)
	}
	if n != uintptr(len(data)) {
		return fmt.Errorf("WriteProcessMemory: wrote %d of %d bytes", n, len(data))
	}
	if flushCode {
		// BOOL FlushInstructionCache(HANDLE hProcess, LPCVOID lpBaseAddress, SIZE_T dwSize)
		if r, _, e := procFlushInstrCache.Call(uintptr(proc), addr, uintptr(len(data))); r == 0 {
			return fmt.Errorf("FlushInstructionCache: %v", e)
		}
	}
	return nil
}
