package sweatbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Wrapper smoke tests: full parse coverage lives in pkg/twrfiles.
// These keep sweatbox.ParseAPT / aliases green for callers and fixture path wiring.

func kbtvFixture(t *testing.T, name string) []byte {
	t.Helper()
	// Canonical fixtures live in pkg/twrfiles/testdata (package dir or module root).
	candidates := []string{
		filepath.Join("..", "..", "pkg", "twrfiles", "testdata", name),
		filepath.Join("pkg", "twrfiles", "testdata", name),
	}
	var last error
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return data
		}
		last = err
	}
	t.Fatalf("read fixture %s: %v", name, last)
	return nil
}

func TestParseAPT_KBTVFixture(t *testing.T) {
	data := kbtvFixture(t, "KBTV_example.apt")
	apt, errs := ParseAPT(string(data))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if apt.ICAO != "KBTV" {
		t.Errorf("ICAO = %q, want KBTV", apt.ICAO)
	}
	// Type aliases + FindSurface method on twrfiles.Airport.
	rwy := apt.FindSurface("19")
	if rwy == nil || rwy.Kind != SurfaceRunway {
		t.Fatalf("FindSurface(19) = %+v", rwy)
	}
	g1 := apt.FindSurface("g1")
	if g1 == nil || g1.Kind != SurfaceParking {
		t.Fatalf("FindSurface(g1) = %+v", g1)
	}
}

func TestParseAPT_InvalidICAO(t *testing.T) {
	_, errs := ParseAPT("icao=BTV\n")
	if len(errs) == 0 {
		t.Fatal("expected ICAO length error")
	}
	if !strings.Contains(errs[0], "ICAO") {
		t.Errorf("err = %q", errs[0])
	}
}

func TestFindSurface_NilAndMiss(t *testing.T) {
	var a *Airport
	if a.FindSurface("X") != nil {
		t.Error("nil airport should return nil")
	}
	apt := Airport{Surfaces: []Surface{{Kind: SurfaceParking, Name: "G1"}}}
	if apt.FindSurface("NOPE") != nil {
		t.Error("miss should be nil")
	}
}

func TestIsWordNameWrapper(t *testing.T) {
	if !isWordName("G1") || isWordName("G-1") || isWordName("") {
		t.Error("isWordName wrapper mismatch")
	}
}
