package pepatch

import (
	"os"
	"path/filepath"
	"testing"
)

// Stock VATSIM FSD JWT URL used by residual full-PE scans (vPilot 3.12.1).
const stockFSDJWT = "https://auth.vatsim.net/api/fsd-jwt"

// TestResidualScan_SyntheticTwoCopies loads a synthetic fixture with two
// UTF-16LE copies of the stock JWT string (one framed like a live #US body,
// one framed like the dead 0xBA44A-style residual). Confirms ScanUTF16String
// reports both — the real PE residual policy must allowlist the dead site
// separately (see docs/client-injector/research/vpilot-3.12.1-gates.md R3).
func TestResidualScan_SyntheticTwoCopies(t *testing.T) {
	path := filepath.Join("testdata", "residual_jwt_two_copies.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	hits := ScanUTF16String(data, stockFSDJWT)
	if len(hits) != 2 {
		t.Fatalf("hits=%v want exactly 2 UTF-16 copies", hits)
	}
	if hits[0] >= hits[1] {
		t.Fatalf("hits not ascending: %v", hits)
	}
	// Fixture layout: first copy after header+prefix; second after mid+framing.
	// Absolute offsets are fixed by the checked-in fixture bytes.
	if hits[0] < 20 {
		t.Fatalf("first hit too early: %d", hits[0])
	}
	// Ensure we matched raw UTF-16 only (no requirement on terminal byte).
	need := len([]byte(stockFSDJWT)) // rune count == byte count for this ASCII string
	_ = need
	for _, h := range hits {
		// Spot-check first UTF-16 code unit is 'h' (0x0068)
		if int(h)+1 >= len(data) {
			t.Fatalf("hit %d OOB", h)
		}
		if data[h] != 'h' || data[h+1] != 0x00 {
			t.Fatalf("hit %d not start of UTF-16LE JWT: %02x %02x", h, data[h], data[h+1])
		}
	}
}

// TestResidualScan_FixtureStable guards the synthetic fixture against
// accidental regeneration that drops a copy (adapter residual tests depend on
// two-hit behavior).
func TestResidualScan_FixtureStable(t *testing.T) {
	path := filepath.Join("testdata", "residual_jwt_two_copies.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 100 {
		t.Fatalf("fixture too small: %d", len(data))
	}
	const magic = "OPENFSD-RESIDUAL-FIXTURE"
	if len(data) < len(magic) || string(data[:len(magic)]) != magic {
		t.Fatalf("fixture magic=%q", data[:min(len(data), 32)])
	}
	hits := ScanUTF16String(data, stockFSDJWT)
	if len(hits) != 2 {
		t.Fatalf("fixture must contain two stock JWT UTF-16 copies; hits=%v", hits)
	}
	// Fixed offsets from generator (magic + NUL + 1-byte prefix = 26).
	if hits[0] != 26 || hits[1] != 113 {
		t.Fatalf("fixture offsets drifted: hits=%v want [26 113]", hits)
	}
}
