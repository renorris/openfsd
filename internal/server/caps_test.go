package server

import (
	"strings"
	"testing"
)

func TestServerCapabilitiesPayload(t *testing.T) {
	p := ServerCapabilitiesPayload()
	if p == "" {
		t.Fatal("payload empty")
	}
	// Required tokens for vatSys CheckCompatabilityWithServer / feature gates
	// that openfsd actually implements (including SECPOS multi-center range).
	for _, flag := range []string{
		"VERSION=1",
		"SECPOS=1",
		"ATCINFO=1",
		"NEWATIS=1",
		"GLOBALDATA=1",
		"ICAOEQ=1",
		"ATCMULTI=1",
		"FASTPOS=1",
	} {
		if !strings.Contains(p, flag) {
			t.Errorf("payload missing %q: %s", flag, p)
		}
	}
	// Wire form is NAME=1 tokens joined by ':' with no empties.
	for _, part := range strings.Split(p, ":") {
		if part == "" {
			t.Fatalf("empty token in payload %q", p)
		}
		if !strings.Contains(part, "=") {
			t.Errorf("token %q missing =", part)
		}
	}
}
