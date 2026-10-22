package client

import (
	"bytes"
	"fmt"

	"golang.org/x/sys/windows"
)

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
