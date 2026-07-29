package clientinject

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/renorris/openfsd/internal/clientinject/pepatch"
)

// Package-level residual fixture (mirrors pepatch/testdata). Ensures engine-
// layer consumers can load the same two-copy stock JWT binary from
// internal/clientinject/testdata without depending on pepatch relative paths.
//
// Layout (keep in sync with pepatch/testdata generator contract):
// live-style terminal 0x01 at first UTF-16 copy; dead residual terminal 0x04
// at second (models vPilot 3.12.1 0xBA44A). Residual HealthCheck allowlists
// the dead class — do not dual-write (design KD-20 / research R3).
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
	u16Len := len(utf16.Encode([]rune(stock))) * 2
	if data[int(hits[0])+u16Len] != 0x01 {
		t.Fatalf("live-style terminal=%#x want 0x01", data[int(hits[0])+u16Len])
	}
	if data[int(hits[1])+u16Len] != 0x04 {
		t.Fatalf("dead-style terminal=%#x want 0x04", data[int(hits[1])+u16Len])
	}
}
