//go:build windows

package account

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropy ties the blob to this app: another program of the same user that calls
// CryptUnprotectData without knowing the string cannot decrypt it by accident.
var entropy = []byte("azoth-launcher-account-v1")

// saveBlob writes the session to account.dat encrypted with the user's DPAPI
// (only this Windows user, on this machine, can read it back).
func saveBlob(dir string, plain []byte) error {
	enc, err := protect(plain)
	if err != nil {
		return err
	}
	return writeAccountFile(dir, enc)
}

// loadBlob reads and decrypts account.dat.
func loadBlob(dir string) ([]byte, error) {
	enc, err := readAccountFile(dir)
	if err != nil {
		return nil, err
	}
	return unprotect(enc)
}

// clearBlob deletes account.dat.
func clearBlob(dir string) error { return removeAccountFile(dir) }

// protect encrypts with the current user's DPAPI (no confirmation dialog).
func protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("nothing to protect")
	}
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(out), nil
}

// unprotect is the inverse of protect.
func unprotect(enc []byte) ([]byte, error) {
	if len(enc) == 0 {
		return nil, errors.New("empty blob")
	}
	in := windows.DataBlob{Size: uint32(len(enc)), Data: &enc[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(out), nil
}

// takeBlob copies the buffer DPAPI allocated and gives it back to the system (LocalFree).
func takeBlob(b windows.DataBlob) []byte {
	if b.Data == nil {
		return nil
	}
	data := append([]byte(nil), unsafe.Slice(b.Data, b.Size)...)
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data)))
	return data
}
