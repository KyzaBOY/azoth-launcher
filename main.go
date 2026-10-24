// Azoth Launcher starts the original New World client against the Azoth fan
// server. It applies a few small patches to the game process in memory (never
// to the game files), signs the player in with Discord and plays from an
// isolated copy of the game: the install is never run, and the only file added
// to it is the Azoth logo pak (internal/client/logo.go).
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend
var assets embed.FS

// Version is the launcher version shown in the footer.
const Version = "0.1.0"

// Build-time settings. The server address is deliberately NOT in the source:
// official builds get it from scripts/release.ps1 through
//
//	wails build -ldflags "-X main.gatewayHost=<host> -X main.gatewayPort=<port>"
//
// A build without them starts, but Play refuses to run. accountURL can be
// overridden the same way (-X main.accountURL=...), e.g. for a local dev build.
var (
	gatewayHost string
	gatewayPort string
	accountURL  = "https://projectazoth.com"
	// regionName replaces "Local Host" in the game menu (up to 40 bytes;
	// "" keeps the original). Override with -X main.regionName=...
	regionName = "NA"
	// loginMode is how the player signs in. "discord" (the default) is the
	// official flow: Discord login, and the Play ticket from the account
	// server becomes the persona. "user" is for test servers whose gateway
	// accepts a plain persona (-auth open): there is no Discord login, the
	// player types a username in the launcher and it becomes the persona, so
	// every username is its own account. Override with -X main.loginMode=user.
	// Any other value counts as "discord" (see effectiveLoginMode), so a typo
	// never opens the username flow by accident.
	loginMode = "discord"
)

func main() {
	app := NewApp()
	opts := &options.App{
		Title:         "Azoth",
		Width:         1280,
		Height:        760,
		MinWidth:      1280,
		MinHeight:     760,
		MaxWidth:      1280,
		MaxHeight:     760,
		DisableResize: true,
		Frameless:     true,
		// same color as the page background, so there is no white flash on open
		BackgroundColour: &options.RGBA{R: 0x08, G: 0x06, B: 0x06, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
	}
	platformOptions(opts)
	if err := wails.Run(opts); err != nil {
		println("Error:", err.Error())
	}
}
