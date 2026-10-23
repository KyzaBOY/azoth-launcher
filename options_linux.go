package main

import (
	_ "embed"
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed build/appicon.png
var appIcon []byte

// webkitDmabufEnv turns off the WebKitGTK DMA-BUF renderer. With it on, some
// Wayland setups (seen on CachyOS) close the window at start with
// "Error 71 (Protocol error) dispatching to Wayland display".
const webkitDmabufEnv = "WEBKIT_DISABLE_DMABUF_RENDERER"

const webkitDmabufDisabled = "1"

// platformOptions sets the Linux window options (WebKitGTK). It runs before
// wails.Run starts GTK, so the WebKit environment is read with our value.
func platformOptions(o *options.App) {
	disableWebkitDmabuf()
	o.Linux = &linux.Options{
		Icon:        appIcon,
		ProgramName: "azoth-launcher",
	}
}

// disableWebkitDmabuf keeps a value the user set, so the renderer can still be
// turned back on with WEBKIT_DISABLE_DMABUF_RENDERER=0.
func disableWebkitDmabuf() {
	if _, set := os.LookupEnv(webkitDmabufEnv); set {
		return
	}
	os.Setenv(webkitDmabufEnv, webkitDmabufDisabled)
}
