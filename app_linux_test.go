package main

import "os"

// dirLink makes link point at target the way the launcher links assets (a symlink).
func dirLink(link, target string) error { return os.Symlink(target, link) }
