package sweatbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAIR_KBTVFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.air"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
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
	if a.Type != "B738/F" {
		t.Errorf("type = %q", a.Type)
	}
	if a.Engine != EngineJet || a.Rules != RulesIFR {
		t.Errorf("engine/rules = %s/%s", a.Engine, a.Rules)
	}
	if a.Dep != "KBTV" || a.Arr != "KBOS" {
		t.Errorf("dep/arr = %s/%s", a.Dep, a.Arr)
	}
	if a.CruiseAlt != 29000 {
		t.Errorf("cruise = %d", a.CruiseAlt)
	}
	if a.Route != "BTV4 MPV LEB MHT" {
		t.Errorf("route = %q", a.Route)
	}
	if a.Remarks != "/v/charts" {
		t.Errorf("remarks = %q", a.Remarks)
	}
	if a.Squawk != "2200" || a.XPDRMode != XPDRModeStandby {
		t.Errorf("sqk = %s mode = %s", a.Squawk, a.XPDRMode)
	}
	if a.Lat != 44.469758 || a.Lon != -73.154747 {
		t.Errorf("pos = %v %v", a.Lat, a.Lon)
	}
	if a.Alt != 335 || a.Speed != 0 || a.Heading != 360 {
		t.Errorf("alt/spd/hdg = %v/%v/%v", a.Alt, a.Speed, a.Heading)
	}

	// Second: turboprop IFR
	if rows[1].Callsign != "USA456" || rows[1].Engine != EngineTurboprop {
		t.Errorf("row1 = %+v", rows[1])
	}
	// Third: piston VFR
	if rows[2].Callsign != "N4729H" || rows[2].Engine != EnginePiston || rows[2].Rules != RulesVFR {
		t.Errorf("row2 = %+v", rows[2])
	}
	if rows[2].Squawk != "1200" {
		t.Errorf("VFR squawk = %s", rows[2].Squawk)
	}
}

func TestParseAIR_CommentsBlankCRLF(t *testing.T) {
	text := "; header\r\n\r\nAAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\r\n"
	rows, errs := ParseAIR(text)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if len(rows) != 1 || rows[0].Callsign != "AAL1" {
		t.Fatalf("rows: %+v", rows)
	}
	if rows[0].XPDRMode != XPDRModeNormal {
		t.Errorf("mode = %s", rows[0].XPDRMode)
	}
}

func TestParseAIR_TooFewFields(t *testing.T) {
	_, errs := ParseAIR("AAL1:B738:J:I:KBTV\n")
	if len(errs) != 1 || !strings.Contains(errs[0], "Invalid number of fields") {
		t.Fatalf("errs: %v", errs)
	}
}

func TestParseAIR_DuplicateCallsign(t *testing.T) {
	line := "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90"
	text := line + "\n" + line + "\n"
	rows, errs := ParseAIR(text)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (first kept)", len(rows))
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "Duplicate callsign") {
		t.Fatalf("errs: %v", errs)
	}
}

func TestParseAIR_InvalidEngine(t *testing.T) {
	line := "AAL1:B738:X:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n"
	_, errs := ParseAIR(line)
	if len(errs) != 1 || !strings.Contains(errs[0], "engine type") {
		t.Fatalf("errs: %v", errs)
	}
}

func TestParseAIR_InvalidRules(t *testing.T) {
	line := "AAL1:B738:J:Z:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n"
	_, errs := ParseAIR(line)
	if len(errs) != 1 || !strings.Contains(errs[0], "flight plan type") {
		t.Fatalf("errs: %v", errs)
	}
}

func TestParseAIR_InvalidXPDR(t *testing.T) {
	line := "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:X:44.0:-73.0:335:0:90\n"
	_, errs := ParseAIR(line)
	if len(errs) != 1 || !strings.Contains(errs[0], "transponder mode") {
		t.Fatalf("errs: %v", errs)
	}
}

func TestParseAIR_InvalidNumeric(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"cruise", "AAL1:B738:J:I:KBTV:KBOS:abc:DCT::2200:N:44.0:-73.0:335:0:90"},
		{"lat", "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:xx:-73.0:335:0:90"},
		{"lon", "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:yy:335:0:90"},
		{"alt", "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:zz:0:90"},
		{"spd", "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:no:90"},
		{"hdg", "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:no"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := ParseAIR(tt.line + "\n")
			if len(errs) != 1 || !strings.Contains(errs[0], "Invalid numeric field") {
				t.Fatalf("errs: %v", errs)
			}
		})
	}
}

func TestParseAIR_MissingCallsign(t *testing.T) {
	line := ":B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90\n"
	_, errs := ParseAIR(line)
	if len(errs) != 1 || !strings.Contains(errs[0], "Missing callsign") {
		t.Fatalf("errs: %v", errs)
	}
}

func TestParseAIR_AllEngineAndRules(t *testing.T) {
	// P/T/J/H and V/I/D/S
	mk := func(cs, eng, rules string) string {
		return cs + ":C172:" + eng + ":" + rules + ":KBTV:KBOS:5000:DCT::1200:N:44.0:-73.0:335:0:90"
	}
	text := strings.Join([]string{
		mk("A1", "P", "V"),
		mk("A2", "T", "I"),
		mk("A3", "J", "D"),
		mk("A4", "H", "S"),
		mk("a5", "p", "v"), // case fold
	}, "\n")
	rows, errs := ParseAIR(text)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[4].Callsign != "A5" || rows[4].Engine != "P" || rows[4].Rules != "V" {
		t.Errorf("case fold: %+v", rows[4])
	}
}

func TestParseAIR_BestEffortPartialLoad(t *testing.T) {
	text := strings.Join([]string{
		"GOOD1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90",
		"BAD:too:few",
		"GOOD2:C172:P:V:KBTV:KLEB:5000:DCT::1200:S:44.1:-73.1:335:0:180",
		"GOOD1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90", // dup
	}, "\n")
	rows, errs := ParseAIR(text)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(rows), rows)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %v, want 2", errs)
	}
	if rows[0].Callsign != "GOOD1" || rows[1].Callsign != "GOOD2" {
		t.Errorf("callsigns: %s %s", rows[0].Callsign, rows[1].Callsign)
	}
}

func TestParseAIR_CruiseAltFloatTrunc(t *testing.T) {
	// python: int(float(f[6]))
	line := "AAL1:B738:J:I:KBTV:KBOS:29000.9:DCT::2200:N:44.0:-73.0:335:0:90\n"
	rows, errs := ParseAIR(line)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if rows[0].CruiseAlt != 29000 {
		t.Errorf("cruise = %d, want 29000", rows[0].CruiseAlt)
	}
}

func TestParseAIR_ExtraFieldsIgnored(t *testing.T) {
	// More than 16 fields: we only use first 16 (split keeps extras in later indices;
	// f[15] is still heading if no colons in heading — extra colons shift fields).
	// With trailing extra colon-fields, heading is still f[15] if we have ≥16.
	line := "AAL1:B738:J:I:KBTV:KBOS:10000:DCT::2200:N:44.0:-73.0:335:0:90:EXTRA:MORE\n"
	rows, errs := ParseAIR(line)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if len(rows) != 1 || rows[0].Heading != 90 {
		t.Fatalf("rows: %+v", rows)
	}
}

func TestParseAIR_InvalidSquawk(t *testing.T) {
	mk := func(cs, sqk string) string {
		return cs + ":B738:J:I:KBTV:KBOS:10000:DCT::" + sqk + ":N:44.0:-73.0:335:0:90\n"
	}
	for _, sqk := range []string{"", "12", "123", "12345", "12A0", "ABCD", "  "} {
		t.Run("bad_"+sqk, func(t *testing.T) {
			rows, errs := ParseAIR(mk("AAL1", sqk))
			if len(rows) != 0 {
				t.Fatalf("want no rows for squawk %q, got %+v", sqk, rows)
			}
			if len(errs) != 1 || !strings.Contains(errs[0], "Invalid squawk code") {
				t.Fatalf("errs: %v", errs)
			}
		})
	}
	// Valid four-digit codes still load.
	rows, errs := ParseAIR(mk("AAL1", "1200") + mk("AAL2", "7700"))
	if len(errs) != 0 || len(rows) != 2 {
		t.Fatalf("valid squawks: rows=%d errs=%v", len(rows), errs)
	}
}
