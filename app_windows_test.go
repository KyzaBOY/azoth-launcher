package main

import "github.com/KyzaBOY/azoth-launcher/internal/client"

// dirLink makes link point at target the way the launcher links assets (a junction).
func dirLink(link, target string) error { return client.CreateJunction(link, target) }
