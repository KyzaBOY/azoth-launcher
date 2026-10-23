// Package config keeps the launcher's settings (the New World folder, where the
// isolated game copy lives and, on Linux, the Proton folder) in config.json
// inside the data directory: %LOCALAPPDATA%\AzothLauncher on Windows,
// $XDG_DATA_HOME/AzothLauncher (~/.local/share/AzothLauncher) on Linux.
// Per-user, no registry, no admin. The same directory holds the account
// session and, unless the player picks another folder, the game copy.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the settings file inside DataDir.
const FileName = "config.json"

// Config is the content of config.json.
type Config struct {
	// GameDir is the folder the player picked: the New World install root
	// (the one with Bin64/NewWorld.exe) or its Bin64 folder. It is read-only
	// for Azoth; the game runs from an isolated copy.
	GameDir string `json:"gameDir"`

	// ProtonPath is the Proton folder umu-run uses (PROTONPATH), e.g. a
	// GE-Proton build (Linux settings; empty = umu picks UMU-Proton).
	ProtonPath string `json:"protonPath,omitempty"`
	// CopyDir is the folder of the isolated game copy (an AzothClient folder
	// inside the one the player picked). Empty means the default,
	// <DataDir>/client.
	CopyDir string `json:"copyDir,omitempty"`
	// Username is the name typed in builds made with the username login
	// (loginMode "user", for test servers): it becomes the persona, and each
	// name is its own account. Unused in the Discord builds.
	Username string `json:"username,omitempty"`
}

// DataDir is <DataHomeEnv>/AzothLauncher, or <home>/<defaultDataHome>/AzothLauncher
// when the variable is not set.
func DataDir() string {
	base := os.Getenv(DataHomeEnv)
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, defaultDataHome)
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "AzothLauncher")
}

// Path is the full path of config.json.
func Path() string { return filepath.Join(DataDir(), FileName) }

// Load reads config.json. A missing file is not an error: it returns the empty
// config. A file that cannot be parsed returns the empty config and an error.
func Load() (Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("invalid %s: %w", FileName, err)
	}
	return c, nil
}

// Save writes config.json atomically (temporary file + rename).
func Save(c Config) error {
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, Path()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
