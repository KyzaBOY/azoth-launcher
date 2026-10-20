package main

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
)

// The error texts below are written for the player: they are shown as they are
// in the status line under Play.
var (
	errNoServer     = errors.New("This build has no server configured. Download the official launcher from projectazoth.com.")
	errBadServer    = errors.New("This build has an invalid server address. Download the official launcher from projectazoth.com.")
	errNotLoggedIn  = errors.New("Log in with Discord to play.")
	errNoFolder     = errors.New("Choose your New World folder in Settings first.")
	errNoGameFound  = errors.New("Couldn't find Bin64\\NewWorld.exe in your New World folder. Check it in Settings.")
	errUnsupported  = errors.New("This New World version isn't supported. Azoth needs build 1.400.6031.")
	errPatchFailed  = errors.New("Couldn't patch the game, so it was not started. Check that New World is build 1.400.6031 and try again.")
	errBadAccountAt = errors.New("This build has an invalid account server address. Download the official launcher from projectazoth.com.")
)

// Progress texts for the status line.
const (
	statusChecking  = "Checking game files…"
	statusPreparing = "Preparing game files (first time only)…"
	statusStarting  = "Starting…"
)

// gateway is the game server this build was compiled for.
type gateway struct {
	host string // host or IP, no port
	addr string // host:port, for -GatewayAddr
}

// parseGateway checks the build-time server settings.
func parseGateway(host, port string) (gateway, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return gateway{}, errNoServer
	}
	if strings.ContainsAny(host, " :/\\\"'") {
		return gateway{}, errBadServer
	}
	p, err := strconv.Atoi(strings.TrimSpace(port))
	if err != nil || p < 1 || p > 65535 {
		return gateway{}, errBadServer
	}
	return gateway{host: host, addr: net.JoinHostPort(host, strconv.Itoa(p))}, nil
}

// prepareGame turns the folder saved in the settings into the Bin64 folder the
// game will run from:
//
//  1. the folder is resolved (the install root, its Bin64 folder or NewWorld.exe);
//  2. NewWorld.exe must be the supported build (normalized sha256);
//  3. if the folder is an install of the game, the game never runs there: it
//     runs from an isolated copy in dataDir, which is built the first time and
//     reused while its exe and its assets link still match the install.
//
// Anything else (a folder that is already an isolated copy) runs in place:
// the patches only ever live in the process memory. status gets progress texts.
func prepareGame(gameDir, dataDir string, status func(string)) (string, error) {
	if status == nil {
		status = func(string) {}
	}
	if strings.TrimSpace(gameDir) == "" {
		return "", errNoFolder
	}
	root, binDir, err := client.ResolveGame(gameDir)
	if err != nil {
		return "", errNoGameFound
	}

	status(statusChecking)
	sum, _, err := client.NormalizedSHA256(filepath.Join(binDir, client.ExeName))
	switch {
	case errors.Is(err, client.ErrTooShort):
		return "", errUnsupported
	case err != nil:
		return "", fmt.Errorf("Couldn't read NewWorld.exe: %w", err)
	case sum != client.ExpectedSHA256:
		return "", errUnsupported
	}

	if !client.IsSteamInstall(binDir) {
		return binDir, nil
	}
	dest := client.IsolatedCopyDir(dataDir)
	if !client.CopyUpToDate(root, dest) {
		status(statusPreparing)
		if _, err := client.Prepare(client.PrepareOptions{GameDir: root, Dest: dest}); err != nil {
			return "", fmt.Errorf("Couldn't prepare the game files: %w", err)
		}
	}
	return filepath.Join(dest, "Bin64"), nil
}

// startError turns the error of client.Launch into the text the player sees. A
// failed patch is explained in a sentence; the technical detail is not shown.
func startError(err error) error {
	if errors.Is(err, client.ErrPatchFailed) {
		return errPatchFailed
	}
	return fmt.Errorf("Couldn't start the game: %w", err)
}
