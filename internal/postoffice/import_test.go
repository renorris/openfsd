package postoffice_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestImportGraph verifies forbidden package edges that prevent import cycles
// and keep session / postoffice independent of server orchestration packages.
//
// Rules (KD-22):
//   - session must not import postoffice
//   - postoffice must not import fsd, web, or a future server package
func TestImportGraph(t *testing.T) {
	const (
		sessionPkg    = "github.com/renorris/openfsd/internal/session"
		postofficePkg = "github.com/renorris/openfsd/internal/postoffice"
	)

	// session ↛ postoffice
	assertNoImport(t, sessionPkg, postofficePkg)

	// postoffice ↛ fsd / web / internal/server (if present)
	for _, forbidden := range []string{
		"github.com/renorris/openfsd/fsd",
		"github.com/renorris/openfsd/web",
		"github.com/renorris/openfsd/internal/server",
	} {
		assertNoImport(t, postofficePkg, forbidden)
	}

	// session must also stay free of fsd/web (cycle prevention for later DI).
	for _, forbidden := range []string{
		"github.com/renorris/openfsd/fsd",
		"github.com/renorris/openfsd/web",
		"github.com/renorris/openfsd/internal/postoffice",
		"github.com/renorris/openfsd/internal/server",
	} {
		assertNoImport(t, sessionPkg, forbidden)
	}
}

// assertNoImport fails if pkgPath has a direct import of forbidden.
// Packages that do not exist yet are skipped (no edge can exist).
func assertNoImport(t *testing.T, pkgPath, forbidden string) {
	t.Helper()

	imports, ok := packageImports(t, pkgPath)
	if !ok {
		return // package not in this module/build yet
	}
	for _, imp := range imports {
		if imp == forbidden {
			t.Errorf("forbidden import edge: %s imports %s", pkgPath, forbidden)
		}
	}
}

// packageImports returns the direct imports of pkgPath.
// ok is false when go list cannot resolve the package (e.g. not yet created).
func packageImports(t *testing.T, pkgPath string) (imports []string, ok bool) {
	t.Helper()

	cmd := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkgPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Missing package is acceptable for optional targets like internal/server.
		msg := stderr.String() + err.Error()
		if strings.Contains(msg, "can't load package") ||
			strings.Contains(msg, "not in std") ||
			strings.Contains(msg, "no required module") ||
			strings.Contains(msg, "directory not found") ||
			strings.Contains(msg, "no Go files") {
			return nil, false
		}
		t.Fatalf("go list %s: %v\n%s", pkgPath, err, stderr.String())
	}

	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			imports = append(imports, line)
		}
	}
	return imports, true
}
