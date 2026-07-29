//go:build !nogui

// Command openfsd-client is the openfsd Client Setup tool.
// With no subcommand, launches the Fyne GUI when a display is available;
// CLI subcommands always run headless.
package main

import (
	"fmt"
	"os"

	"github.com/renorris/openfsd/cmd/openfsd-client/gui"
)

func main() {
	if len(os.Args) <= 1 {
		if gui.CanStart() {
			if err := gui.RunDefault(); err != nil {
				fmt.Fprintf(os.Stderr, "gui: %v\n\n", err)
				// Fall back to CLI help when GUI cannot run.
				os.Exit(Run(nil, os.Stdout, os.Stderr))
			}
			return
		}
		// No display — print help (same as previous headless default).
		os.Exit(Run(nil, os.Stdout, os.Stderr))
	}
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
