// Azoth Launcher starts the original New World client against the Azoth fan
// server. It applies a few small patches to the game process in memory (never
// to the game files), signs the player in with Discord and plays from an
// isolated copy of the game, so the Steam install is never touched.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
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
)

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
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
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			Theme:                windows.Dark,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
