package main

import (
	"github.com/KyzaBOY/azoth-launcher/internal/client"
	"github.com/KyzaBOY/azoth-launcher/internal/config"
)

// launchGame starts the game with the in-memory patches (client.Launch).
func (a *App) launchGame(_ config.Config, opts client.LaunchOptions) (notice string, err error) {
	_, err = client.Launch(opts)
	return "", err
}
