//go:build research

package clientinject

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/renorris/openfsd/internal/clientinject/pepatch"
)

// Optional research-gate revalidation against a local (gitignored) extract.
//
//	go test -tags=research ./internal/clientinject/ -count=1
//
// Looks for OPENFSD_VPILOT_EXE, then common .research paths relative to the
// module / home scratch tree. Skips cleanly when the PE is absent so CI
// without extract stays green under -tags=research.

const (
	researchStockJWT    = "https://auth.vatsim.net/api/fsd-jwt"
	researchStockAFV    = "https://voice1.vatsim.net"
	researchWantSHA1    = "7d95a7110392c15728143cc30e1f00899c686eb5"
	researchJWTBodyOff  = int64(0xB7988)
	researchJWTDeadOff  = int64(0xBA44A)
	researchJWTLdstrOff = int64(0x4BDB5)
	researchAFVBodyOff  = int64(0xA9076)
	researchAFVLdstrOff = int64(0x1F0A7)
	researchUSHeapOff   = int64(0xA22E8)
	researchUSHeapSize  = int64(0x15E04)
	researchJWTHeapOff  = 0x1569F
	researchAFVHeapOff  = 0x6D8D
)

func researchVPilotPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("OPENFSD_VPILOT_EXE"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		t.Fatalf("OPENFSD_VPILOT_EXE=%q not a file", p)
	}
	candidates := []string{
		filepath.Join(".research", "vpilot", "extracted", "vPilot.exe"),
		filepath.Join("..", "..", ".research", "vpilot", "extracted", "vPilot.exe"),
		"/Users/rnorris/scratch/openfsd/.research/vpilot/extracted/vPilot.exe",
	}
	// Also try walking up from cwd for monorepo root .research/
	wd, _ := os.Getwd()
	for i := 0; i < 6 && wd != ""; i++ {
		candidates = append(candidates, filepath.Join(wd, ".research", "vpilot", "extracted", "vPilot.exe"))
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	t.Skip("vPilot 3.12.1 extract not found (set OPENFSD_VPILOT_EXE or place under .research/vpilot/extracted/)")
	return ""
}

func TestResearch_VPilot3121_FingerprintsAndSites(t *testing.T) {
	path := researchVPilotPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(data)
	got := hex.EncodeToString(sum[:])
	if got != researchWantSHA1 {
		t.Fatalf("sha1=%s want %s (wrong version?)", got, researchWantSHA1)
	}
	if int64(len(data)) != 1236416 {
		t.Fatalf("size=%d want 1236416", len(data))
	}

	// Residual UTF-16: exactly primary + dead second site.
	hits := pepatch.ScanUTF16String(data, researchStockJWT)
	if len(hits) != 2 {
		t.Fatalf("JWT residual hits=%v want 2", hits)
	}
	if hits[0] != researchJWTBodyOff || hits[1] != researchJWTDeadOff {
		t.Fatalf("JWT hits=%v want [%#x %#x]", hits, researchJWTBodyOff, researchJWTDeadOff)
	}

	// Live #US body terminal 0x01; dead site terminal 0x04.
	if term := data[researchJWTBodyOff+int64(len(researchStockJWT)*2)]; term != 0x01 {
		t.Fatalf("live JWT terminal=%#x want 0x01", term)
	}
	if term := data[researchJWTDeadOff+int64(len(researchStockJWT)*2)]; term != 0x04 {
		t.Fatalf("dead JWT terminal=%#x want 0x04", term)
	}
	// Dead site is outside #US heap.
	if researchJWTDeadOff >= researchUSHeapOff && researchJWTDeadOff < researchUSHeapOff+researchUSHeapSize {
		t.Fatal("dead JWT site unexpectedly inside #US heap")
	}
	if researchJWTBodyOff < researchUSHeapOff || researchJWTBodyOff >= researchUSHeapOff+researchUSHeapSize {
		t.Fatal("live JWT body not inside #US heap")
	}

	// ldstr tokens
	assertLdstr(t, data, researchJWTLdstrOff, researchJWTHeapOff)
	assertLdstr(t, data, researchAFVLdstrOff, researchAFVHeapOff)

	// AFV body present once in UTF-16
	afvHits := pepatch.ScanUTF16String(data, researchStockAFV)
	if len(afvHits) < 1 || afvHits[0] != researchAFVBodyOff {
		t.Fatalf("AFV hits=%v want primary %#x", afvHits, researchAFVBodyOff)
	}

	// 3.11.1 AFV ret site must NOT be a lone 0x2A on this build.
	if data[0x4BA54] == 0x2A {
		t.Fatalf("unexpected ret at obsolete 3.11.1 AFV site 0x4BA54 — re-check R2")
	}
}

func TestResearch_VPilot3121_GeoVRNoVoiceHost(t *testing.T) {
	pe := researchVPilotPath(t)
	dir := filepath.Dir(pe)
	for _, name := range []string{"GeoVR.Client.dll", "GeoVR.Connection.dll", "GeoVR.Shared.dll"} {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			t.Skipf("GeoVR DLL missing (%s): %v", p, err)
		}
		if hits := pepatch.ScanUTF16String(data, "voice1.vatsim.net"); len(hits) != 0 {
			t.Fatalf("%s has UTF-16 voice1.vatsim.net at %v (R5 regression)", name, hits)
		}
		if idx := indexASCII(data, "voice1.vatsim.net"); idx >= 0 {
			t.Fatalf("%s has ASCII voice1.vatsim.net at %d (R5 regression)", name, idx)
		}
	}
}

func assertLdstr(t *testing.T, data []byte, fileOff int64, heapOff int) {
	t.Helper()
	if fileOff+5 > int64(len(data)) {
		t.Fatalf("ldstr OOB %#x", fileOff)
	}
	if data[fileOff] != 0x72 {
		t.Fatalf("ldstr opcode at %#x = %#x", fileOff, data[fileOff])
	}
	tok := binary.LittleEndian.Uint32(data[fileOff+1:])
	want := uint32(0x70000000 | heapOff)
	if tok != want {
		t.Fatalf("ldstr token at %#x = %#x want %#x", fileOff, tok, want)
	}
}

func indexASCII(data []byte, s string) int {
	pat := []byte(s)
	for i := 0; i+len(pat) <= len(data); i++ {
		ok := true
		for j := range pat {
			if data[i+j] != pat[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
