package twrfiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAPT_KBTVFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.apt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	apt, errs := ParseAPT(string(data))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if apt.ICAO != "KBTV" {
		t.Errorf("ICAO = %q, want KBTV", apt.ICAO)
	}
	if apt.MagVar != 16 {
		t.Errorf("MagVar = %v, want 16", apt.MagVar)
	}
	if apt.FieldElev != 335 {
		t.Errorf("FieldElev = %v, want 335", apt.FieldElev)
	}
	if apt.PatternElev != 1335 {
		t.Errorf("PatternElev = %v, want 1335", apt.PatternElev)
	}
	if apt.PatternSize != 1 {
		t.Errorf("PatternSize = %v, want 1", apt.PatternSize)
	}
	if apt.InitClimbProps != 10000 {
		t.Errorf("InitClimbProps = %v, want 10000", apt.InitClimbProps)
	}
	if apt.InitClimbJets != 10000 {
		t.Errorf("InitClimbJets = %v, want 10000", apt.InitClimbJets)
	}
	if apt.Registration != "N" {
		t.Errorf("Registration = %q, want N", apt.Registration)
	}
	if !strings.Contains(apt.JetAirlines, "AAL") {
		t.Errorf("JetAirlines missing AAL: %q", apt.JetAirlines)
	}
	if !strings.Contains(apt.TurboAirlines, "EGF") {
		t.Errorf("TurboAirlines missing EGF: %q", apt.TurboAirlines)
	}

	// Count surface kinds.
	var nPark, nRwy, nTaxi, nHold int
	for _, s := range apt.Surfaces {
		switch s.Kind {
		case SurfaceParking:
			nPark++
			if len(s.Points) != 1 {
				t.Errorf("parking %s: %d points", s.Name, len(s.Points))
			}
		case SurfaceRunway:
			nRwy++
			if len(s.Points) < 2 {
				t.Errorf("runway %s: %d points", s.Name, len(s.Points))
			}
		case SurfaceTaxiway:
			nTaxi++
		case SurfaceHold:
			nHold++
		}
	}
	if nPark != 16 {
		t.Errorf("parking count = %d, want 16", nPark)
	}
	if nRwy != 2 {
		t.Errorf("runway count = %d, want 2", nRwy)
	}
	if nTaxi != 12 {
		t.Errorf("taxiway count = %d, want 12", nTaxi)
	}
	if nHold != 0 {
		t.Errorf("hold count = %d, want 0", nHold)
	}

	// Runway 19/1 options.
	rwy := apt.FindSurface("19")
	if rwy == nil {
		t.Fatal("FindSurface(19) = nil")
	}
	if rwy.Name != "19/1" || rwy.RwyA != "19" || rwy.RwyB != "1" {
		t.Errorf("runway 19/1 fields: %+v", rwy)
	}
	if rwy.DispA != 0 || rwy.DispB != 225 {
		t.Errorf("displaced threshold = %v/%v, want 0/225", rwy.DispA, rwy.DispB)
	}
	if rwy.TurnoffLeft {
		t.Error("turnoff=right expected TurnoffLeft=false")
	}

	rwy2 := apt.FindSurface("33/15")
	if rwy2 == nil {
		t.Fatal("FindSurface(33/15) = nil")
	}
	if rwy2.DispA != 500 || rwy2.DispB != 0 {
		t.Errorf("33/15 disp = %v/%v, want 500/0", rwy2.DispA, rwy2.DispB)
	}
	if !rwy2.TurnoffLeft {
		t.Error("turnoff=left expected TurnoffLeft=true")
	}

	// Parking lookup case-insensitive.
	g1 := apt.FindSurface("g1")
	if g1 == nil || g1.Kind != SurfaceParking {
		t.Fatalf("FindSurface(g1) = %+v", g1)
	}
	if g1.Points[0].Lat < 44.46 || g1.Points[0].Lon > -73.15 {
		t.Errorf("G1 point unexpected: %+v", g1.Points[0])
	}

	// Taxiway.
	tw := apt.FindSurface("A")
	if tw == nil || tw.Kind != SurfaceTaxiway || len(tw.Points) < 2 {
		t.Fatalf("taxiway A: %+v", tw)
	}
}

func TestParseAPT_Defaults(t *testing.T) {
	apt, errs := ParseAPT("icao=KXYZ\n")
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if apt.PatternSize != defaultPatternSize {
		t.Errorf("PatternSize default = %v", apt.PatternSize)
	}
	if apt.InitClimbProps != defaultInitClimbProps {
		t.Errorf("InitClimbProps default = %v", apt.InitClimbProps)
	}
	if apt.InitClimbJets != defaultInitClimbJets {
		t.Errorf("InitClimbJets default = %v", apt.InitClimbJets)
	}
	if apt.Registration != DefaultRegistration {
		t.Errorf("Registration default = %q", apt.Registration)
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

func TestParseAPT_CommentsAndBlankAndCRLF(t *testing.T) {
	text := "; comment\r\n\r\nicao=KBTV\r\nmagnetic variation=16\r\n"
	apt, errs := ParseAPT(text)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if apt.ICAO != "KBTV" || apt.MagVar != 16 {
		t.Errorf("got %+v", apt)
	}
}

func TestParseAPT_InvalidNumericHeaders(t *testing.T) {
	text := strings.Join([]string{
		"icao=KBTV",
		"magnetic variation=abc",
		"field elevation=xyz",
		"pattern elevation=?",
		"pattern size=nope",
		"initial climb props=bad",
		"initial climb jets=bad",
	}, "\n")
	_, errs := ParseAPT(text)
	if len(errs) < 6 {
		t.Fatalf("want ≥6 numeric errors, got %v", errs)
	}
}

func TestParseAPT_ParkingWaypointCounts(t *testing.T) {
	text := `
icao=KBTV
[PARKING G1]
; no waypoint
[PARKING G2]
44.1 -73.1
44.2 -73.2
`
	_, errs := ParseAPT(text)
	var noWP, extraWP int
	for _, e := range errs {
		if strings.Contains(e, "has no waypoint defined") {
			noWP++
		}
		if strings.Contains(e, "Extra waypoint found in parking") {
			extraWP++
		}
	}
	if noWP != 1 || extraWP != 1 {
		t.Fatalf("want 1 missing + 1 extra parking error, got %v", errs)
	}
}

func TestParseAPT_RunwayValidation(t *testing.T) {
	text := `
icao=KBTV
[RUNWAY 19/1]
44.1 -73.1
[RUNWAY 99/1]
[RUNWAY 22/04]
[RUNWAY 15L/33R]
44.0 -73.0
44.1 -73.1
`
	apt, errs := ParseAPT(text)
	// 19/1 has only 1 point → error
	// 99/1 invalid designator → unknown line
	// 22/04 leading zero on B → unknown
	// 15L/33R valid with 2 points → ok
	var hasRwyPts, hasUnknown bool
	for _, e := range errs {
		if strings.Contains(e, "Runway 19/1") {
			hasRwyPts = true
		}
		if strings.Contains(e, "Unknown line") {
			hasUnknown = true
		}
	}
	if !hasRwyPts {
		t.Errorf("missing runway waypoint error: %v", errs)
	}
	if !hasUnknown {
		t.Errorf("missing unknown line for bad runway: %v", errs)
	}
	if apt.FindSurface("15L") == nil {
		t.Error("expected 15L/33R to parse")
	}
}

func TestParseAPT_TaxiwayAndHold(t *testing.T) {
	text := `
icao=KBTV
[TAXIWAY A]
44.1 -73.1
[TAXIWAY B1]
44.0 -73.0
44.1 -73.1
[HOLD HS1]
44.5 -73.5
[HOLD HS2]
[HOLD HS3]
44.0 -73.0
44.1 -73.1
[TAXIWAY 1BAD]
[HOLD bad-name]
`
	apt, errs := ParseAPT(text)
	// A has 1 point → taxi error; HS2 no point → hold error; HS3 extra → hold extra; bad names → unknown
	var taxiErr, holdNone, holdExtra, unknown int
	for _, e := range errs {
		switch {
		case strings.Contains(e, "Taxiway A"):
			taxiErr++
		case strings.Contains(e, "Hold HS2 has no waypoint"):
			holdNone++
		case strings.Contains(e, "Extra waypoint found in hold section HS3"):
			holdExtra++
		case strings.Contains(e, "Unknown line"):
			unknown++
		}
	}
	if taxiErr != 1 {
		t.Errorf("taxi err count %d: %v", taxiErr, errs)
	}
	if holdNone != 1 {
		t.Errorf("hold missing err count %d: %v", holdNone, errs)
	}
	if holdExtra != 1 {
		t.Errorf("hold extra err count %d: %v", holdExtra, errs)
	}
	if unknown < 2 {
		t.Errorf("want ≥2 unknown (bad names), got %d: %v", unknown, errs)
	}
	if apt.FindSurface("B1") == nil {
		t.Error("B1 missing")
	}
	if apt.FindSurface("HS1") == nil {
		t.Error("HS1 missing")
	}
}

func TestParseAPT_Duplicates(t *testing.T) {
	text := `
icao=KBTV
[PARKING G1]
44.1 -73.1
[PARKING G1]
44.2 -73.2
[TAXIWAY A]
44.0 -73.0
44.1 -73.1
[TAXIWAY A]
44.0 -73.0
44.1 -73.1
[RUNWAY 1/19]
44.0 -73.0
44.1 -73.1
[RUNWAY 1/19]
44.0 -73.0
44.1 -73.1
[HOLD H1]
44.5 -73.5
[HOLD H1]
44.6 -73.6
`
	_, errs := ParseAPT(text)
	var dups int
	for _, e := range errs {
		if strings.Contains(e, "Duplicate") {
			dups++
		}
	}
	if dups != 4 {
		t.Fatalf("want 4 duplicate errors, got %d: %v", dups, errs)
	}
}

func TestParseAPT_TaxiHoldSharedNamespace(t *testing.T) {
	// TWRTrainer: taxiway and hold share one name space.
	text := `
icao=KBTV
[TAXIWAY A]
44.0 -73.0
44.1 -73.1
[HOLD A]
44.5 -73.5
[HOLD B]
44.6 -73.6
[TAXIWAY B]
44.0 -73.0
44.1 -73.1
`
	_, errs := ParseAPT(text)
	var dups int
	for _, e := range errs {
		if strings.Contains(e, "Duplicate taxiway or hold") {
			dups++
		}
	}
	if dups != 2 {
		t.Fatalf("want 2 shared-namespace dups (A and B), got %d: %v", dups, errs)
	}
}

func TestParseAPT_RegistrationAndAirlineLists(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr string
	}{
		{
			name:    "registration too long",
			text:    "icao=KBTV\nregistration=N123\n",
			wantErr: "Invalid registration prefix",
		},
		{
			name:    "registration empty",
			text:    "icao=KBTV\nregistration=\n",
			wantErr: "Invalid registration prefix",
		},
		{
			name:    "jet airline too long",
			text:    "icao=KBTV\njet airlines=TOOLONGPREFIX,AAL\n",
			wantErr: "Invalid list of jet airlines",
		},
		{
			name:    "jet airline single letter",
			text:    "icao=KBTV\njet airlines=A,AAL\n",
			wantErr: "Invalid list of jet airlines",
		},
		{
			name:    "turboprop empty segment mid-list",
			text:    "icao=KBTV\nturboprop airlines=EGF,,USA\n",
			wantErr: "Invalid list of turboprop airlines",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := ParseAPT(tt.text)
			found := false
			for _, e := range errs {
				if strings.Contains(e, tt.wantErr) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want err containing %q, got %v", tt.wantErr, errs)
			}
		})
	}

	// Valid cases: single letter, 2–3 char prefixes, trailing comma, empty list.
	okText := `
icao=KBTV
registration=N
jet airlines=AAL,ACA,ML,
turboprop airlines=
`
	apt, errs := ParseAPT(okText)
	if len(errs) != 0 {
		t.Fatalf("valid headers: %v", errs)
	}
	if apt.Registration != "N" || apt.JetAirlines != "AAL,ACA,ML," {
		t.Errorf("apt headers: reg=%q jet=%q", apt.Registration, apt.JetAirlines)
	}
}

func TestParseAPT_InvalidTurnoff(t *testing.T) {
	text := `
icao=KBTV
[RUNWAY 9/27]
turnoff=sideways
44.0 -73.0
44.1 -73.1
`
	apt, errs := ParseAPT(text)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "Invalid turnoff direction") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want invalid turnoff error: %v", errs)
	}
	r := apt.FindSurface("9")
	if r == nil || !r.TurnoffLeft {
		t.Errorf("invalid turnoff should leave default left: %+v", r)
	}
}

func TestParseAPT_TurnoffAndDisplacedOutsideRunway(t *testing.T) {
	text := `
icao=KBTV
turnoff=left
displaced threshold=100/200
[PARKING G1]
44.1 -73.1
turnoff=right
`
	_, errs := ParseAPT(text)
	// turnoff without runway, displaced without runway, turnoff after parking
	if len(errs) < 2 {
		t.Fatalf("want errors for orphaned runway options: %v", errs)
	}
}

func TestParseAPT_UnknownAndOrphanPoint(t *testing.T) {
	text := `
icao=KBTV
not a valid line
44.1 -73.1
`
	_, errs := ParseAPT(text)
	if len(errs) < 2 {
		t.Fatalf("want ≥2 errors, got %v", errs)
	}
}

func TestParseAPT_DisplacedThresholdFormats(t *testing.T) {
	text := `
icao=KBTV
[RUNWAY 9/27]
displaced threshold=100/200
44.0 -73.0
44.1 -73.1
`
	apt, errs := ParseAPT(text)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	r := apt.FindSurface("9")
	if r == nil || r.DispA != 100 || r.DispB != 200 {
		t.Fatalf("disp: %+v", r)
	}
}

func TestParseAPT_CaseInsensitiveHeaders(t *testing.T) {
	text := `
ICAO=kbtv
Magnetic Variation=14.5
Field Elevation=100
Pattern Elevation=1100
Pattern Size=1.5
Initial Climb Props=2000
Initial Climb Jets=4000
Jet Airlines=UAL,DAL
Turboprop Airlines=SKW
Registration=N
[parking ga1]
44.1 -73.1
[runway 18/36]
turnoff=LEFT
44.0 -73.0
44.1 -73.1
[taxiway a]
44.0 -73.0
44.1 -73.1
[hold hs1]
44.2 -73.2
`
	apt, errs := ParseAPT(text)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if apt.ICAO != "KBTV" {
		t.Errorf("ICAO=%q", apt.ICAO)
	}
	if apt.MagVar != 14.5 || apt.FieldElev != 100 || apt.PatternElev != 1100 {
		t.Errorf("headers: %+v", apt)
	}
	if apt.PatternSize != 1.5 || apt.InitClimbProps != 2000 || apt.InitClimbJets != 4000 {
		t.Errorf("climbs: %+v", apt)
	}
	if apt.JetAirlines != "UAL,DAL" || apt.TurboAirlines != "SKW" {
		t.Errorf("airlines jet=%q turbo=%q", apt.JetAirlines, apt.TurboAirlines)
	}
	if apt.FindSurface("GA1") == nil || apt.FindSurface("18") == nil {
		t.Error("surfaces missing")
	}
	r := apt.FindSurface("18")
	if r == nil || !r.TurnoffLeft {
		t.Errorf("turnoff left: %+v", r)
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

func TestParseAPT_InvalidDisplacedThreshold(t *testing.T) {
	// parseDisplacedThreshold returns false for bad format → unknown line if no match.
	// With a runway current, malformed displaced that doesn't parse as displaced falls through.
	// Integers only (TWRTrainer ^(\d+)/(\d+)$); floats rejected.
	for _, bad := range []string{
		"displaced threshold=abc/def",
		"displaced threshold=100.5/200",
		"displaced threshold=-1/0",
	} {
		text := "icao=KBTV\n[RUNWAY 1/19]\n" + bad + "\n44.0 -73.0\n44.1 -73.1\n"
		_, errs := ParseAPT(text)
		found := false
		for _, e := range errs {
			if strings.Contains(e, "Unknown line") {
				found = true
			}
		}
		if !found {
			t.Fatalf("want unknown line for %q: %v", bad, errs)
		}
	}
}

func TestIsRunwayDesignator(t *testing.T) {
	valid := []string{"1", "9", "10", "18", "27", "36", "15L", "33R", "9C", "36L"}
	for _, s := range valid {
		if !IsRunwayDesignator(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	invalid := []string{"", "0", "37", "01", "100", "L", "1X", "22/4"}
	for _, s := range invalid {
		if IsRunwayDesignator(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestParsePoint(t *testing.T) {
	lat, lon, ok := parsePoint("44.1 -73.2")
	if !ok || lat != 44.1 || lon != -73.2 {
		t.Fatalf("got %v %v %v", lat, lon, ok)
	}
	// Integers without decimal rejected.
	if _, _, ok := parsePoint("44 -73"); ok {
		t.Error("integers should fail")
	}
	if _, _, ok := parsePoint("44.1"); ok {
		t.Error("single field should fail")
	}
	if _, _, ok := parsePoint("-.1 -73.0"); ok {
		t.Error("missing digits before dot")
	}
	if _, _, ok := parsePoint("44. -73.0"); ok {
		t.Error("missing digits after dot")
	}
	if _, _, ok := parsePoint("4a.1 -73.0"); ok {
		t.Error("non-digit before dot")
	}
	if _, _, ok := parsePoint("44.1b -73.0"); ok {
		t.Error("non-digit after dot")
	}
	if _, _, ok := parsePoint(""); ok {
		t.Error("empty should fail")
	}
}

func TestParseAPT_InvalidParkingName(t *testing.T) {
	// Name with punctuation fails IsWordName after matchSection.
	text := "icao=KBTV\n[PARKING G-1]\n44.1 -73.1\n"
	_, errs := ParseAPT(text)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "Unknown line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want unknown for bad parking name: %v", errs)
	}
}

func TestHelperEdgeCases(t *testing.T) {
	// parseFloatField missing '='
	if _, err := parseFloatField("noequals"); err == nil {
		t.Error("expected missing = error")
	}

	// parseDisplacedThreshold: not a prefix; wrong slash count
	if _, _, ok := parseDisplacedThreshold("nope"); ok {
		t.Error("expected false for non-prefix")
	}
	if _, _, ok := parseDisplacedThreshold("displaced threshold=100"); ok {
		t.Error("expected false for single value")
	}
	if _, _, ok := parseDisplacedThreshold("displaced threshold=1/2/3"); ok {
		t.Error("expected false for three parts")
	}

	// matchSection edge cases
	if _, ok := matchSection("x", "PARKING"); ok {
		t.Error("short line")
	}
	if _, ok := matchSection("[PARKING]", "PARKING"); ok {
		t.Error("one field only")
	}
	if _, ok := matchSection("[OTHER X]", "PARKING"); ok {
		t.Error("wrong kind")
	}
	if _, ok := matchSection("PARKING X]", "PARKING"); ok {
		t.Error("missing open bracket")
	}

	// matchRunway edge cases
	if _, _, ok := matchRunway("x"); ok {
		t.Error("short")
	}
	if _, _, ok := matchRunway("[RUNWAY]"); ok {
		t.Error("one field")
	}
	if _, _, ok := matchRunway("[TAXIWAY A]"); ok {
		t.Error("not runway kind")
	}
	if _, _, ok := matchRunway("[RUNWAY 19]"); ok {
		t.Error("no slash")
	}

	// IsWordName / IsTaxiHoldName
	if IsWordName("") {
		t.Error("empty word")
	}
	if IsWordName("G-1") {
		t.Error("hyphen not word")
	}
	if IsWordName("G_1") != true {
		t.Error("underscore allowed")
	}
	if IsTaxiHoldName("") {
		t.Error("empty taxi")
	}
	if IsTaxiHoldName("A1B") {
		t.Error("letter after digit")
	}

	// hasDecimalPoint empty
	if hasDecimalPoint("") {
		t.Error("empty decimal")
	}
	if hasDecimalPoint("-") {
		t.Error("bare minus")
	}

	// registration / airline list helpers
	if !isValidRegistration("N") || isValidRegistration("N1") || isValidRegistration("") {
		t.Error("registration validator")
	}
	if !isValidAirlineList("") || !isValidAirlineList("AAL") || !isValidAirlineList("AAL,ACA,") {
		t.Error("valid airline lists rejected")
	}
	if isValidAirlineList("TOOLONG") || isValidAirlineList("A") || isValidAirlineList("AAL,,B") {
		t.Error("invalid airline lists accepted")
	}
	if !isDigits("0") || !isDigits("100") || isDigits("") || isDigits("1.0") || isDigits("-1") {
		t.Error("isDigits")
	}
}
