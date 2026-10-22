package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// platformOptions sets the Windows window options.
func platformOptions(o *options.App) {
	o.Windows = &windows.Options{
		WebviewIsTransparent: false,
		WindowIsTranslucent:  false,
		DisableWindowIcon:    false,
		Theme:                windows.Dark,
	}
}
