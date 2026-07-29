package pepatch

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// Stock VATSIM FSD JWT URL used by residual full-PE scans (vPilot 3.12.1).
const stockFSDJWT = "https://auth.vatsim.net/api/fsd-jwt"

// Fixture layout (residual_jwt_two_copies.bin), generator contract:
//
//	magic "OPENFSD-RESIDUAL-FIXTURE" (24) + NUL     → bytes 0..24
//	live-style prefix 0x47 (fake #US length=71)    → byte 25
//	UTF-16 stock JWT + terminal 0x01 (live-style)  → hit[0]=26, term @ 26+70
//	mid junk DE AD BE EF 00 00                     → after live body
//	dead framing (8 2D 43 1C EB E2 36 0A 3F 46)    → like PE 0xBA44A pre-context
//	UTF-16 stock JWT + terminal 0x04 (dead-style)  → hit[1]=113, term @ 113+70
//
// Residual HealthCheck (post-R3) allowlists documented dead terminals/offsets;
// it must not require dual-writing the dead copy (design KD-20).

func utf16ByteLen(s string) int {
	return len(utf16.Encode([]rune(s))) * 2
}

// TestResidualScan_SyntheticTwoCopies loads a synthetic fixture with two
// UTF-16LE copies of the stock JWT string (live-style + dead residual framing).
// Confirms ScanUTF16String reports both — the real PE residual policy must
// allowlist the dead site separately (see research gates R3 / design KD-20).
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
	if hits[0] < 20 {
		t.Fatalf("first hit too early: %d", hits[0])
	}
	u16Len := utf16ByteLen(stockFSDJWT)
	for i, h := range hits {
		if int(h)+1 >= len(data) {
			t.Fatalf("hit %d OOB", h)
		}
		if data[h] != 'h' || data[h+1] != 0x00 {
			t.Fatalf("hit %d not start of UTF-16LE JWT: %02x %02x", h, data[h], data[h+1])
		}
		termOff := int(h) + u16Len
		if termOff >= len(data) {
			t.Fatalf("hit %d missing terminal byte", h)
		}
		wantTerm := byte(0x01)
		if i == 1 {
			wantTerm = 0x04 // dead residual framing (R3-style)
		}
		if data[termOff] != wantTerm {
			t.Fatalf("hit[%d] terminal at %d = %#x want %#x", i, termOff, data[termOff], wantTerm)
		}
	}
}

// TestResidualScan_FixtureStable guards the synthetic fixture against
// accidental regeneration that drops a copy or changes live/dead terminals.
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
	// Fixed offsets from generator (magic 24 + NUL + 1-byte prefix → body @ 26; second @ 113).
	if hits[0] != 26 || hits[1] != 113 {
		t.Fatalf("fixture offsets drifted: hits=%v want [26 113]", hits)
	}
	u16Len := utf16ByteLen(stockFSDJWT)
	// Live-style terminal after first copy; dead-style after second (R3 narrative).
	if data[int(hits[0])+u16Len] != 0x01 {
		t.Fatalf("live-style terminal = %#x want 0x01", data[int(hits[0])+u16Len])
	}
	if data[int(hits[1])+u16Len] != 0x04 {
		t.Fatalf("dead-style terminal = %#x want 0x04", data[int(hits[1])+u16Len])
	}
	// Live copy is immediately after a 1-byte fake #US length prefix at offset 25.
	if data[25] != 0x47 {
		t.Fatalf("live-style prefix at 25 = %#x want 0x47", data[25])
	}
}
