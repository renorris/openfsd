// Command download-xp12-aptdat fetches X-Plane 12 Global Airports apt.dat onto
// the local machine only. openfsd does not redistribute that data.
//
// Licensing / preferred sources: docs/xplane-airport-data.md
//
// Prefer integrating download into the converter:
//
//	go run ./cmd/aptdat2apt -download -out generated-apt -quiet
//
// Standalone usage (same underlying library: internal/xp12aptdat):
//
//	go run ./cmd/download-xp12-aptdat -out ./xp12-aptdat
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/renorris/openfsd/internal/xp12aptdat"
)

func main() {
	outDir := flag.String("out", xp12aptdat.DefaultOutDir, "output directory")
	keepZip := flag.Bool("keep-zip", false, "keep apt.dat.zip after extract")
	directoryOnly := flag.Bool("directory-only", false, "download directory.txt.zip only")
	branchPref := flag.String("branch", xp12aptdat.DefaultBranch, "preferred branch: Final, Beta, or RSG")
	lookupURL := flag.String("lookup", xp12aptdat.DefaultLookupURL, "server list URL")
	flag.Parse()

	res, err := xp12aptdat.Download(xp12aptdat.Options{
		OutDir:        *outDir,
		KeepZip:       *keepZip,
		DirectoryOnly: *directoryOnly,
		Branch:        *branchPref,
		LookupURL:     *lookupURL,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if res.AptPath != "" {
		fmt.Println()
		fmt.Println("Use with aptdat2apt, e.g.:")
		fmt.Printf("  go run ./cmd/aptdat2apt -aptdat %s -out generated-apt -quiet\n", res.AptPath)
		fmt.Println("Or download+convert in one step:")
		fmt.Println("  go run ./cmd/aptdat2apt -download -out generated-apt -quiet")
	}
}
