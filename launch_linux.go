package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/config"
	"github.com/KyzaBOY/azoth-launcher/internal/wine"
)

// patcherSHA256 is the sha256 of the azoth-patcher.exe this launcher ships
// with, set at build time (scripts/release-linux.sh):
//
//	-ldflags "-X main.patcherSHA256=<hex>"
//
// A build without it refuses to hand the ticket to any helper.
var patcherSHA256 string

// Texts for the player, Linux only (see flow.go).
var (
	errNoUmu       = errors.New("umu-launcher is not installed. See the README for how to install it.")
	errNoPatcher   = errors.New("azoth-patcher.exe is missing next to the launcher. Download the official launcher from projectazoth.com.")
	errBadPatcher  = errors.New("azoth-patcher.exe does not match this launcher. Download the official launcher from projectazoth.com.")
	errNoPatcherID = errors.New("This build cannot check azoth-patcher.exe. Download the official launcher from projectazoth.com.")
)

const (
	statusProton      = "Setting up Proton (first time only)…"
	noticePingBlocked = "Game started. The menu ping will show 9999 ms: this system blocks ping for normal users (net.ipv4.ping_group_range). See the README."
)

// launchGame runs azoth-patcher.exe in Wine/Proton through umu-run; the helper
// does what client.Launch does on Windows. The prefix and the helper's copy
// live in the data directory.
func (a *App) launchGame(cfg config.Config, opts client.LaunchOptions) (notice string, err error) {
	runner, err := exec.LookPath(wine.Runner)
	if err != nil {
		return "", errNoUmu
	}
	dataDir := config.DataDir()
	helper := filepath.Join(dataDir, "bin", wine.HelperName)
	if err := installPatcher(helper); err != nil {
		return "", err
	}
	prefix := filepath.Join(dataDir, "prefix")
	if _, err := os.Stat(prefix); err != nil {
		a.setStatus(statusProton)
	}
	rep, err := wine.Launch(a.ctx, wine.Options{
		Runner:     runner,
		Helper:     helper,
		Prefix:     prefix,
		ProtonPath: cfg.ProtonPath,
		RunDir:     filepath.Join(dataDir, "run"),
		Launch:     opts,
	})
	switch {
	case err != nil:
		return "", err
	case rep.PatchFailed: // the helper killed the process before it ran
		return "", fmt.Errorf("%w: %s", client.ErrPatchFailed, rep.Error)
	case !rep.OK:
		return "", errors.New(rep.Error)
	}
	if ok, err := wine.PingAllowed(); err == nil && !ok {
		return noticePingBlocked, nil
	}
	return "", nil
}

// installPatcher puts the helper shipped next to the launcher into dst after
// checking it against patcherSHA256.
func installPatcher(dst string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	err = wine.InstallHelper(filepath.Join(filepath.Dir(exe), wine.HelperName), dst, patcherSHA256)
	switch {
	case errors.Is(err, wine.ErrNoHelperChecksum):
		return errNoPatcherID
	case errors.Is(err, wine.ErrHelperMissing):
		return errNoPatcher
	case errors.Is(err, wine.ErrHelperMismatch):
		return errBadPatcher
	}
	return err
}
