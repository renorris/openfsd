package postoffice_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestImportGraph verifies forbidden package edges (including transitive deps)
// that prevent import cycles and keep session / postoffice independent of
// server orchestration packages.
//
// Rules (KD-22):
//   - session must not depend on postoffice (direct or transitive)
//   - postoffice must not depend on fsd, web, or a future server package
//   - session must not depend on fsd, web, or server
//
// Required source packages (session, postoffice) must load; failure is fatal.
// Optional forbidden targets (e.g. internal/server) need not exist yet — they
// are only matched if they appear in the source package's dep closure.
func TestImportGraph(t *testing.T) {
	const (
		sessionPkg    = "github.com/renorris/openfsd/internal/session"
		postofficePkg = "github.com/renorris/openfsd/internal/postoffice"
	)

	// session ↛ postoffice / fsd / web / server (dep closure)
	assertNoDeps(t, sessionPkg, []string{
		postofficePkg,
		"github.com/renorris/openfsd/fsd",
		"github.com/renorris/openfsd/web",
		"github.com/renorris/openfsd/internal/web",
		"github.com/renorris/openfsd/internal/server",
	})

	// postoffice ↛ fsd / web / server (dep closure)
	assertNoDeps(t, postofficePkg, []string{
		"github.com/renorris/openfsd/fsd",
		"github.com/renorris/openfsd/web",
		"github.com/renorris/openfsd/internal/web",
		"github.com/renorris/openfsd/internal/server",
	})
}

// assertNoDeps fails if sourcePkg's go list -deps closure contains any path in forbidden.
// sourcePkg is required: load failure is fatal (not soft-skipped).
func assertNoDeps(t *testing.T, sourcePkg string, forbidden []string) {
	t.Helper()

	deps := packageDeps(t, sourcePkg)
	depSet := make(map[string]struct{}, len(deps))
	for _, d := range deps {
		depSet[d] = struct{}{}
	}

	for _, f := range forbidden {
		if _, hit := depSet[f]; hit {
			t.Errorf("forbidden import edge (dep closure): %s depends on %s", sourcePkg, f)
		}
	}
}

// packageDeps returns non-standard package paths in the dependency closure of pkgPath
// (equivalent to: go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}').
// Fails the test if pkgPath cannot be loaded.
func packageDeps(t *testing.T, pkgPath string) []string {
	t.Helper()

	// -deps walks the full import graph; filter out stdlib so we only see module edges.
	cmd := exec.Command("go", "list", "-deps", "-f", `{{if not .Standard}}{{.ImportPath}}{{end}}`, pkgPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -deps %s (required source package): %v\n%s", pkgPath, err, stderr.String())
	}

	var deps []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// go list -deps includes the package itself; skip self for clarity.
		if line == pkgPath {
			continue
		}
		deps = append(deps, line)
	}
	return deps
}
