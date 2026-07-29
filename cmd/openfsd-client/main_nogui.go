//go:build nogui

// Headless build: CLI only (no Fyne). Use: go build -tags nogui ./cmd/openfsd-client
package main

import "os"

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
