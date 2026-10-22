package account

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileName is the session file inside the data directory (%LOCALAPPDATA%\AzothLauncher
// on Windows; on Linux only when there is no Secret Service).
const FileName = "account.dat"

// Save stores the session where only this user can read it back: encrypted
// with DPAPI on Windows, in the Secret Service keyring on Linux (see saveBlob).
func Save(dir string, s Session) error {
	plain, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return saveBlob(dir, plain)
}

// Load reads the session. false = it does not exist, it cannot be decrypted
// (another user/machine, corrupted file) or it has already expired.
func Load(dir string) (Session, bool) {
	plain, err := loadBlob(dir)
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

// Clear deletes the stored session (logout). A missing session is not an error.
func Clear(dir string) error { return clearBlob(dir) }

// writeAccountFile writes account.dat atomically, readable by this user only.
func writeAccountFile(dir string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, FileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func readAccountFile(dir string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, FileName))
}

// removeAccountFile deletes account.dat. A missing file is not an error.
func removeAccountFile(dir string) error {
	err := os.Remove(filepath.Join(dir, FileName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
