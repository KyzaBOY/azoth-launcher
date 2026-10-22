package wine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// HelperName is the helper's file name, next to the launcher and in the data
// directory.
const HelperName = "azoth-patcher.exe"

var (
	// ErrNoHelperChecksum: the build has no sha256 for the helper, so no
	// helper is trusted with the ticket.
	ErrNoHelperChecksum = errors.New("no helper checksum in this build")
	// ErrHelperMissing: the helper is not next to the launcher.
	ErrHelperMissing = errors.New("helper missing")
	// ErrHelperMismatch: the helper is not the one this launcher was built with.
	ErrHelperMismatch = errors.New("helper checksum mismatch")
)

// InstallHelper copies the helper from src (next to the launcher) to dst (in
// the data directory, where Wine is known to see it) unless dst is already
// the same file. src must have the sha256 the launcher was built with.
func InstallHelper(src, dst, wantSHA256 string) error {
	if wantSHA256 == "" {
		return ErrNoHelperChecksum
	}
	sum, err := fileSHA256(src)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrHelperMissing, src)
	}
	if err != nil {
		return err
	}
	if sum != wantSHA256 {
		return fmt.Errorf("%w: %s", ErrHelperMismatch, src)
	}
	if cur, err := fileSHA256(dst); err == nil && cur == wantSHA256 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
