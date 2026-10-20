package account

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileName is the session file inside the data directory (%LOCALAPPDATA%\AzothLauncher).
const FileName = "account.dat"

// Save writes the session encrypted with the user's DPAPI (only this Windows
// user, on this machine, can read it back). The write is atomic.
func Save(dir string, s Session) error {
	plain, err := json.Marshal(s)
	if err != nil {
		return err
	}
	enc, err := protect(plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, FileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, enc, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Load reads the session. false = it does not exist, it cannot be decrypted
// (another user/machine, corrupted file) or it has already expired.
func Load(dir string) (Session, bool) {
	enc, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return Session{}, false
	}
	plain, err := unprotect(enc)
	if err != nil {
		return Session{}, false
	}
	var s Session
	if err := json.Unmarshal(plain, &s); err != nil {
		return Session{}, false
	}
	if s.Token == "" || s.Expires <= time.Now().Unix() {
		return Session{}, false
	}
	return s, true
}

// Clear deletes the stored session (logout). A missing file is not an error.
func Clear(dir string) error {
	err := os.Remove(filepath.Join(dir, FileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
