package client

import (
	"os"
	"path/filepath"
)

// linkKind names the assets link in messages.
const linkKind = "symlink"

// linkDir makes link a symlink to the absolute target. Wine follows it like
// any folder.
func linkDir(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	return os.Symlink(abs, link)
}

// pathKey is the form two paths are compared in: Linux paths are case-sensitive.
func pathKey(p string) string { return filepath.Clean(p) }

func sameDir(a, b string) bool { return pathKey(a) == pathKey(b) }
