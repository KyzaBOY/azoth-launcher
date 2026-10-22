package client

import (
	"bytes"
	"fmt"

	"golang.org/x/sys/windows"
)

// applyRedirect writes the string in the slack (checking first that it is
// zeroed), replaces the code and reads both back. Any surprise aborts before
// the code is written: the string alone in the slack changes nothing in the game.
func applyRedirect(proc windows.Handle, base uintptr, r StringRedirect, value string, logf func(string, ...any)) error {
	if err := validRedirectValue(value); err != nil {
		return fmt.Errorf("[%s] %w", r.Name, err)
	}
	if r.SlotRVA < RdataSlackStart || r.SlotRVA+MaxRedirectLen+1 > RdataSlackEnd || len(r.Orig) != 7+len(r.Tail) {
		return fmt.Errorf("[%s] bad redirect definition", r.Name)
	}
	site := base + uintptr(r.SiteRVA)
	slot := base + uintptr(r.SlotRVA)

	cur := make([]byte, len(r.Orig))
	if err := readMem(proc, site, cur); err != nil {
		return fmt.Errorf("[%s] read code at 0x%X: %w", r.Name, site, err)
	}
	logf("[%s] code at base+0x%X (0x%X): %s", r.Name, r.SiteRVA, site, Hex(cur))
	if !bytes.Equal(cur, r.Orig) {
		return fmt.Errorf("[%s] %w: %s (expected %s)", r.Name, errUnexpectedBytes, Hex(cur), Hex(r.Orig))
	}

	for _, c := range r.Also {
		b := make([]byte, len(c.Orig))
		if err := readMem(proc, base+uintptr(c.RVA), b); err != nil {
			return fmt.Errorf("[%s] read code at base+0x%X: %w", r.Name, c.RVA, err)
		}
		if !bytes.Equal(b, c.Orig) || len(c.New) != len(c.Orig) {
			return fmt.Errorf("[%s] %w at base+0x%X: %s (expected %s)", r.Name, errUnexpectedBytes, c.RVA, Hex(b), Hex(c.Orig))
		}
	}

	slack := make([]byte, MaxRedirectLen+1)
	if err := readMem(proc, slot, slack); err != nil {
		return fmt.Errorf("[%s] read .rdata slack at 0x%X: %w", r.Name, slot, err)
	}
	if !bytes.Equal(slack, make([]byte, len(slack))) {
		return fmt.Errorf("[%s] .rdata slack at 0x%X is not zeroed (different build?)", r.Name, slot)
	}
	str := append([]byte(value), 0)
	if err := writeMem(proc, slot, str, windows.PAGE_READWRITE, false); err != nil {
		return fmt.Errorf("[%s] write string: %w", r.Name, err)
	}
	back := make([]byte, len(str))
	if err := readMem(proc, slot, back); err != nil || !bytes.Equal(back, str) {
		return fmt.Errorf("[%s] %w (string)", r.Name, errPatchNotConfirmed)
	}

	for _, c := range r.Also {
		a := base + uintptr(c.RVA)
		if err := writeMem(proc, a, c.New, windows.PAGE_EXECUTE_READWRITE, true); err != nil {
			return fmt.Errorf("[%s] write code at 0x%X: %w", r.Name, a, err)
		}
		b := make([]byte, len(c.New))
		if err := readMem(proc, a, b); err != nil || !bytes.Equal(b, c.New) {
			return fmt.Errorf("[%s] %w at 0x%X", r.Name, errPatchNotConfirmed, a)
		}
		logf("[%s] code at base+0x%X: %s -> %s", r.Name, c.RVA, Hex(c.Orig), Hex(c.New))
	}

	want := r.Patched()
	if err := writeMem(proc, site, want, windows.PAGE_EXECUTE_READWRITE, true); err != nil {
		return fmt.Errorf("[%s] write code: %w", r.Name, err)
	}
	after := make([]byte, len(want))
	if err := readMem(proc, site, after); err != nil || !bytes.Equal(after, want) {
		return fmt.Errorf("[%s] %w: read %s", r.Name, errPatchNotConfirmed, Hex(after))
	}
	logf("[%s] code -> %s (lea rsi -> 0x%X, %d bytes of string)", r.Name, Hex(after), slot, len(value))
	return nil
}
