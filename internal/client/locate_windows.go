package client

import (
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// steamRoots returns the Steam install folders the registry knows:
// HKCU\Software\Valve\Steam\SteamPath and HKLM\...\Valve\Steam\InstallPath.
func steamRoots() []string {
	var roots []string
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue("SteamPath"); err == nil && v != "" {
			roots = append(roots, filepath.Clean(v))
		}
		k.Close()
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue("InstallPath"); err == nil && v != "" {
			roots = append(roots, filepath.Clean(v))
		}
		k.Close()
	}
	return roots
}
