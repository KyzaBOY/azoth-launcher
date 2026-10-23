package main

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/config"
)

// The error texts below are written for the player: they are shown as they are
// in the status line under Play.
var (
	errNoServer     = errors.New("This build has no server configured. Download the official launcher from projectazoth.com.")
	errBadServer    = errors.New("This build has an invalid server address. Download the official launcher from projectazoth.com.")
	errNotLoggedIn  = errors.New("Log in with Discord to play.")
	errNoFolder     = errors.New("Choose your New World folder in Settings first.")
	errNoGameFound  = errors.New("Couldn't find " + filepath.Join("Bin64", client.ExeName) + " in your New World folder. Check it in Settings.")
	errUnsupported  = errors.New("This New World version isn't supported. Azoth needs build 1.400.6031.")
	errPatchFailed  = errors.New("Couldn't patch the game, so it was not started. Check that New World is build 1.400.6031 and try again.")
	errBadAccountAt = errors.New("This build has an invalid account server address. Download the official launcher from projectazoth.com.")

	errNoUsername       = errors.New("Type a username to play.")
	errUsernameChars    = errors.New("Usernames can only use letters, numbers, _ and -.")
	errUsernameLen      = fmt.Errorf("Usernames must have %d to %d characters.", usernameMin, usernameMax)
	errUsernameReserved = errors.New("That username isn't allowed. Pick another one.")
)

// How the player signs in (the loginMode build setting, see main.go).
const (
	loginModeDiscord = "discord"
	loginModeUser    = "user"
)

// effectiveLoginMode is loginMode as the rest of the launcher reads it: "user"
// only for exactly that value, "discord" for everything else.
func effectiveLoginMode() string {
	if strings.EqualFold(strings.TrimSpace(loginMode), loginModeUser) {
		return loginModeUser
	}
	return loginModeDiscord
}

// The username becomes the game's persona (client.PersonaRedirect), which is
// copied into a slot of client.MaxRedirectLen bytes: 24 fit with a wide margin.
const (
	usernameMin = 3
	usernameMax = 24
	// ticketPrefix starts every Play ticket; a persona that starts with it is
	// read as a ticket by the gateway, so a username can't.
	ticketPrefix = "az1-"
)

// validateUsername trims s and checks it as a username: 3..24 characters of
// [A-Za-z0-9_-], not starting with ticketPrefix (any case). It returns the
// trimmed name, which is what goes into the game.
func validateUsername(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errNoUsername
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return "", errUsernameChars
		}
	}
	if len(s) < usernameMin || len(s) > usernameMax {
		return "", errUsernameLen
	}
	if len(s) >= len(ticketPrefix) && strings.EqualFold(s[:len(ticketPrefix)], ticketPrefix) {
		return "", errUsernameReserved
	}
	return s, nil
}

// copyFolderName is the folder the game copy gets inside the folder the player
// picks for it, so the copy never spreads its files over that folder.
const copyFolderName = "AzothClient"

// copyRoot is the folder of the isolated game copy for these settings.
func copyRoot(c config.Config) string {
	if c.CopyDir == "" {
		return client.IsolatedCopyDir(config.DataDir())
	}
	return c.CopyDir
}

// copyDirFor turns the folder the player picked for the copy into the copy's
// own folder: <picked>\AzothClient, or the pick itself when it already is one.
func copyDirFor(picked string) string {
	p := filepath.Clean(picked)
	if strings.EqualFold(filepath.Base(p), copyFolderName) {
		return p
	}
	return filepath.Join(p, copyFolderName)
}

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
//  3. the Azoth logo pak goes into the install's assets folder (the one file
//     the launcher adds to an install; see client.InstallLogo);
//  4. if the folder is an install of the game, the game never runs there: it
//     runs from an isolated copy in copyDir, which is built the first time and
//     reused while its exe and its assets link still match the install.
//
// Anything else (a folder that is already an isolated copy) runs in place:
// the patches only ever live in the process memory. status gets progress texts.
func prepareGame(gameDir, copyDir string, status func(string)) (string, error) {
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

	// The Azoth logo at the end of the tutorial (internal/client/logo.go). It is
	// cosmetic: if it cannot be written, the game still starts with its own logo.
	_ = client.InstallLogo(filepath.Join(root, "assets"))

	if !client.IsSteamInstall(binDir) {
		return binDir, nil
	}
	dest := copyDir
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
