package client

import (
	"os"
	"path/filepath"
)

// steamHomes are the places Steam lives on Linux, relative to the home folder:
// the native package, its legacy link and the Flatpak.
var steamHomes = []string{
	filepath.Join(".local", "share", "Steam"),
	filepath.Join(".steam", "steam"),
	filepath.Join(".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
}

// steamRoots returns the Steam folders that exist, each once: ~/.steam/steam is
// usually a symlink to ~/.local/share/Steam, so the real paths are compared.
func steamRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var roots []string
	seen := map[string]bool{}
	for _, rel := range steamHomes {
		real, err := filepath.EvalSymlinks(filepath.Join(home, rel))
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		roots = append(roots, real)
	}
	return roots
}
