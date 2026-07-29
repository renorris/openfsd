package clientinject

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject/pepatch"
)

// Package-level residual fixture (mirrors pepatch/testdata). Ensures engine-
// layer consumers can load the same two-copy stock JWT binary from
// internal/clientinject/testdata without depending on pepatch relative paths.
func TestResidualFixture_TwoUTF16Copies(t *testing.T) {
	path := filepath.Join("testdata", "residual_jwt_two_copies.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v (synthetic residual fixtures must be committed)", err)
	}
	const stock = "https://auth.vatsim.net/api/fsd-jwt"
	hits := pepatch.ScanUTF16String(data, stock)
	if len(hits) != 2 {
		t.Fatalf("hits=%v want 2", hits)
	}
	if hits[0] != 26 || hits[1] != 113 {
		t.Fatalf("hits=%v want [26 113]", hits)
	}
}
