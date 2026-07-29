// Command openfsd-client is the openfsd Client Setup tool (headless CLI).
// GUI (Fyne) lands in a later PR; with no subcommand this binary prints help.
package main

import (
	"os"
)

func main() {
	os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr))
}
