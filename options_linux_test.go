package main

import (
	"os"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestPlatformOptionsDisablesWebkitDmabuf(t *testing.T) {
	t.Setenv(webkitDmabufEnv, "")
	os.Unsetenv(webkitDmabufEnv)

	platformOptions(&options.App{})

	if got := os.Getenv(webkitDmabufEnv); got != webkitDmabufDisabled {
		t.Fatalf("%s = %q, want %q", webkitDmabufEnv, got, webkitDmabufDisabled)
	}
}

func TestPlatformOptionsKeepsUserWebkitDmabuf(t *testing.T) {
	const userValue = "0"
	t.Setenv(webkitDmabufEnv, userValue)

	platformOptions(&options.App{})

	if got := os.Getenv(webkitDmabufEnv); got != userValue {
		t.Fatalf("%s = %q, want %q", webkitDmabufEnv, got, userValue)
	}
}
