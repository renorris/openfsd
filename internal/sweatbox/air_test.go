package sweatbox

import (
	"strings"
	"testing"
)

// Wrapper smoke tests: full parse coverage lives in pkg/twrfiles.

func TestParseAIR_KBTVFixture(t *testing.T) {
	data := kbtvFixture(t, "KBTV_example.air")
	rows, errs := ParseAIR(string(data))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	a := rows[0]
	if a.Callsign != "AAL123" {
		t.Errorf("callsign = %q", a.Callsign)
	}
	if a.Engine != EngineJet || a.Rules != RulesIFR {
		t.Errorf("engine/rules = %s/%s", a.Engine, a.Rules)
	}
	if a.Squawk != "2200" || a.XPDRMode != XPDRModeStandby {
		t.Errorf("sqk = %s mode = %s", a.Squawk, a.XPDRMode)
	}
}

func TestParseAIR_InvalidEngine(t *testing.T) {
	line := "AAL1:B738:X:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n"
	_, errs := ParseAIR(line)
	if len(errs) != 1 || !strings.Contains(errs[0], "engine type") {
		t.Fatalf("errs: %v", errs)
	}
}
