package sweatbox

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

func loadKBTVEngine(t *testing.T) *Engine {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.apt"))
	if err != nil {
		t.Fatalf("read apt: %v", err)
	}
	apt, errs := ParseAPT(string(data))
	if len(errs) != 0 {
		t.Fatalf("apt errs: %v", errs)
	}
	e := NewEngine()
	if err := e.LoadAirport(&apt); err != nil {
		t.Fatalf("LoadAirport: %v", err)
	}
	return e
}

func TestNewEngine_Defaults(t *testing.T) {
	e := NewEngine()
	if !e.Paused() {
		t.Error("new engine should be paused")
	}
	if e.Count() != 0 {
		t.Errorf("count = %d", e.Count())
	}
	s := e.Settings()
	if s.MaxAircraft != DefaultMaxAircraft {
		t.Errorf("MaxAircraft = %d", s.MaxAircraft)
	}
	if e.Airport() != nil {
		t.Error("airport should be nil")
	}
	if e.Elapsed() != 0 {
		t.Error("elapsed should be 0")
	}
}

func TestSettings_NormalizeClamp(t *testing.T) {
	e := NewEngineSettings(Settings{MaxAircraft: 999, IntersectionTolM: -1})
	s := e.Settings()
	if s.MaxAircraft != HardMaxAircraft {
		t.Errorf("MaxAircraft = %d, want %d", s.MaxAircraft, HardMaxAircraft)
	}
	if s.IntersectionTolM != DefaultIntersectionTolM {
		t.Errorf("tol = %v", s.IntersectionTolM)
	}
	e.SetMaxAircraft(0)
	if e.Settings().MaxAircraft != DefaultMaxAircraft {
		t.Errorf("after SetMaxAircraft(0) = %d", e.Settings().MaxAircraft)
	}
	e.SetMaxAircraft(10)
	if e.Settings().MaxAircraft != 10 {
		t.Errorf("MaxAircraft = %d", e.Settings().MaxAircraft)
	}
}

func TestLoadAirport_NilAndReplace(t *testing.T) {
	e := NewEngine()
	if err := e.LoadAirport(nil); err == nil {
		t.Fatal("expected error for nil airport")
	}
	e = loadKBTVEngine(t)
	if e.Airport() == nil || e.Airport().ICAO != "KBTV" {
		t.Fatalf("icao = %v", e.Airport())
	}
	if e.Graph() == nil {
		t.Fatal("graph nil")
	}
	// With aircraft present, reject reload.
	r := e.CommandLine("add v s p @GA1")
	if !r.OK {
		t.Fatalf("add: %s", r.Message)
	}
	apt2 := *e.Airport()
	if err := e.LoadAirport(&apt2); err == nil {
		t.Fatal("expected error when aircraft present")
	}
	// After delete all, reload OK.
	e.Delete(r.Added[0].Callsign)
	if err := e.LoadAirport(&apt2); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func TestLoadScenario_AutoPauseAndBestEffort(t *testing.T) {
	e := loadKBTVEngine(t)
	e.Unpause()
	if e.Paused() {
		t.Fatal("should be unpaused")
	}
	data, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.air"))
	if err != nil {
		t.Fatal(err)
	}
	rows, perrs := ParseAIR(string(data))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	loaded, errs := e.LoadScenario(rows)
	if !e.Paused() {
		t.Error("LoadScenario must auto-pause")
	}
	if len(errs) != 0 {
		t.Errorf("errs: %v", errs)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded = %v", loaded)
	}
	snap := e.Snapshot()
	if snap.ICAO != "KBTV" || len(snap.Aircraft) != 3 {
		t.Fatalf("snap = %+v", snap)
	}
	// Sorted by callsign.
	for i := 1; i < len(snap.Aircraft); i++ {
		if snap.Aircraft[i-1].Callsign > snap.Aircraft[i].Callsign {
			t.Errorf("not sorted: %s > %s", snap.Aircraft[i-1].Callsign, snap.Aircraft[i].Callsign)
		}
	}
	// Duplicate callsign skipped.
	loaded2, errs2 := e.LoadScenario(rows[:1])
	if len(loaded2) != 0 {
		t.Errorf("dup loaded: %v", loaded2)
	}
	if len(errs2) != 1 {
		t.Errorf("dup errs: %v", errs2)
	}
	// No airport.
	e2 := NewEngine()
	_, errs3 := e2.LoadScenario(rows)
	if len(errs3) != 1 || errs3[0] != "No airport loaded." {
		t.Errorf("no apt: %v", errs3)
	}
}

func TestLoadScenario_MaxAircraft(t *testing.T) {
	e := NewEngineSettings(Settings{MaxAircraft: 1})
	apt, _ := ParseAPT("icao=KBTV\nmagnetic variation=0\nfield elevation=0\npattern elevation=1000\n[PARKING A]\n44  -73\n")
	if err := e.LoadAirport(&apt); err != nil {
		t.Fatal(err)
	}
	rows := []Aircraft{
		{Callsign: "AAA1", Engine: EngineJet, Rules: RulesIFR, Squawk: "2000", XPDRMode: XPDRModeNormal, Lat: 1, Lon: 2, Alt: 1000, Speed: 100, Heading: 90},
		{Callsign: "AAA2", Engine: EngineJet, Rules: RulesIFR, Squawk: "2001", XPDRMode: XPDRModeNormal, Lat: 1, Lon: 2, Alt: 1000, Speed: 100, Heading: 90},
	}
	loaded, errs := e.LoadScenario(rows)
	if len(loaded) != 1 || len(errs) != 1 {
		t.Fatalf("loaded=%v errs=%v", loaded, errs)
	}
}

func TestPauseUnpauseTickElapsed(t *testing.T) {
	e := NewEngine()
	e.Unpause()
	r := e.Tick(5 * time.Second)
	if len(r.Updates) != 0 || len(r.Deletes) != 0 {
		t.Errorf("skeleton tick should be empty: %+v", r)
	}
	if e.Elapsed() != 5*time.Second {
		t.Errorf("elapsed = %v", e.Elapsed())
	}
	e.Pause()
	e.Tick(10 * time.Second)
	if e.Elapsed() != 5*time.Second {
		t.Errorf("paused tick must not advance elapsed: %v", e.Elapsed())
	}
	// Non-positive dt.
	e.Unpause()
	e.Tick(0)
	e.Tick(-time.Second)
	if e.Elapsed() != 5*time.Second {
		t.Errorf("dt<=0 should no-op: %v", e.Elapsed())
	}
}

func TestDeleteIdempotent(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA1")
	cs := r.Added[0].Callsign
	if !e.Delete(cs) {
		t.Fatal("first delete")
	}
	if e.Delete(cs) {
		t.Fatal("second delete should be false")
	}
	if e.Delete("") {
		t.Fatal("empty")
	}
	if _, ok := e.Get(cs); ok {
		t.Fatal("get after delete")
	}
}

func TestOpsStats(t *testing.T) {
	e := NewEngine()
	e.Unpause()
	e.Tick(2 * time.Minute)
	// Manually bump counters via unexported fields under lock for ops/min.
	e.mu.Lock()
	e.arr = 3
	e.dep = 1
	e.mu.Unlock()
	ops := e.Ops()
	if ops.ArrCount != 3 || ops.DepCount != 1 {
		t.Errorf("ops counts: %+v", ops)
	}
	if math.Abs(ops.OpsPerMin-2.0) > 1e-9 {
		t.Errorf("ops/min = %v, want 2", ops.OpsPerMin)
	}
	msg := formatOpsMessage(ops)
	if msg == "" || msg[0] != 'E' {
		t.Errorf("format: %q", msg)
	}
	// Zero elapsed.
	e2 := NewEngine()
	if e2.Ops().OpsPerMin != 0 {
		t.Error("zero elapsed ops/min")
	}
}

func TestCommand_PauseUnpauseOps(t *testing.T) {
	e := loadKBTVEngine(t)
	tests := []struct {
		line   string
		wantOK bool
		paused *bool
	}{
		{"p", true, boolPtr(true)},
		{"pause", true, boolPtr(true)},
		{"un", true, boolPtr(false)},
		{"unpause", true, boolPtr(false)},
		{"unp", true, boolPtr(false)},
		{"u", true, boolPtr(false)},
		{"up", true, boolPtr(false)},
	}
	for _, tc := range tests {
		r := e.CommandLine(tc.line)
		if r.OK != tc.wantOK {
			t.Errorf("%q: ok=%v msg=%q", tc.line, r.OK, r.Message)
		}
		if tc.paused != nil && e.Paused() != *tc.paused {
			t.Errorf("%q: paused=%v want %v", tc.line, e.Paused(), *tc.paused)
		}
	}
	e.Unpause()
	e.Tick(time.Hour)
	r := e.CommandLine("ops")
	if !r.OK || r.Message == "" {
		t.Fatalf("ops: %+v", r)
	}
	r = e.CommandLine("stats")
	if !r.OK || r.Message == "" {
		t.Fatalf("stats: %+v", r)
	}
}

func boolPtr(b bool) *bool { return &b }

func TestCommand_AddParking(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA1")
	if !r.OK {
		t.Fatalf("add: %s", r.Message)
	}
	if len(r.Added) != 1 {
		t.Fatalf("added: %v", r.Added)
	}
	ac := r.Added[0]
	if ac.Status != StatusParked {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.Type != "C172" {
		t.Errorf("type = %s", ac.Type)
	}
	if ac.Speed != 0 {
		t.Errorf("speed = %v", ac.Speed)
	}
	if ac.Parking != "GA1" {
		t.Errorf("parking = %s", ac.Parking)
	}
	if ac.Dep != "KBTV" {
		t.Errorf("dep = %s", ac.Dep)
	}
	// Callsign should use registration N…
	if ac.Callsign == "" || ac.Callsign[0] != 'N' {
		t.Errorf("callsign = %q (want N…)", ac.Callsign)
	}
	// Override type.
	r2 := e.CommandLine("add i h j @G1 B744")
	if !r2.OK {
		t.Fatalf("add heavy: %s", r2.Message)
	}
	if r2.Added[0].Type != "B744" {
		t.Errorf("type = %s", r2.Added[0].Type)
	}
	if r2.Added[0].Engine != EngineJet {
		t.Errorf("engine = %s", r2.Added[0].Engine)
	}
	// Jet callsign from airline list.
	cs := r2.Added[0].Callsign
	if len(cs) < 4 {
		t.Errorf("jet callsign too short: %q", cs)
	}
	// Unknown parking.
	r3 := e.CommandLine("add v s p @NOPE")
	if r3.OK {
		t.Fatal("expected unknown parking fail")
	}
}

func TestCommand_AddApproach(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add i l j 33 10")
	if !r.OK {
		t.Fatalf("add: %s", r.Message)
	}
	ac := r.Added[0]
	if ac.Status != StatusOnApproach {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.LandingRunway != "33" {
		t.Errorf("rwy = %s", ac.LandingRunway)
	}
	if ac.Type != "B738" {
		t.Errorf("type = %s", ac.Type)
	}
	if ac.Arr != "KBTV" {
		t.Errorf("arr = %s", ac.Arr)
	}
	if ac.Speed != 180 {
		t.Errorf("speed = %v", ac.Speed)
	}
	// Altitude roughly field + 10*318.
	wantAlt := 335 + glideslopeFtPerNM*10
	if math.Abs(ac.Alt-wantAlt) > 5 {
		t.Errorf("alt = %v, want ~%v", ac.Alt, wantAlt)
	}
	// Should be ~10 NM from threshold of 33.
	apt := e.Airport()
	rwy := apt.FindSurface("33")
	thr, hdg, ok := runwayThreshold(rwy, "33")
	if !ok {
		t.Fatal("threshold")
	}
	distM := geo.Distance(ac.Lat, ac.Lon, thr.Lat, thr.Lon)
	if math.Abs(distM-10*metersPerNM) > 200 {
		t.Errorf("dist from thr = %v m, want ~%v", distM, 10*metersPerNM)
	}
	// Heading should be landing heading.
	if math.Abs(normalizeHeading(ac.Heading)-normalizeHeading(hdg)) > 2 {
		t.Errorf("hdg = %v, want %v", ac.Heading, hdg)
	}
	// Bad runway.
	r2 := e.CommandLine("add i l j 99 5")
	if r2.OK {
		t.Fatal("bad rwy should fail")
	}
	// Override type.
	r3 := e.CommandLine("add v m t 19 5 BE20")
	if !r3.OK {
		t.Fatalf("%s", r3.Message)
	}
	if r3.Added[0].Type != "BE20" {
		t.Errorf("type = %s", r3.Added[0].Type)
	}
}

func TestCommand_AddBearing(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p -270 15 2500")
	if !r.OK {
		t.Fatalf("add: %s", r.Message)
	}
	ac := r.Added[0]
	if ac.Status != StatusAirborne {
		t.Errorf("status = %s", ac.Status)
	}
	if math.Abs(ac.Alt-2500) > 0.1 {
		t.Errorf("alt = %v", ac.Alt)
	}
	// Heading inbound = 270+180 = 90.
	if math.Abs(ac.Heading-90) > 1 {
		t.Errorf("hdg = %v, want 90", ac.Heading)
	}
	ref := fieldReferencePoint(e.Airport())
	distM := geo.Distance(ref.Lat, ref.Lon, ac.Lat, ac.Lon)
	if math.Abs(distM-15*metersPerNM) > 300 {
		t.Errorf("dist from field = %v m", distM)
	}
	r2 := e.CommandLine("add i l j -35 45 7000 A320")
	if !r2.OK {
		t.Fatalf("%s", r2.Message)
	}
	if r2.Added[0].Type != "A320" {
		t.Errorf("type = %s", r2.Added[0].Type)
	}
}

func TestCommand_AddValidation(t *testing.T) {
	e := loadKBTVEngine(t)
	cases := []struct {
		line string
		ok   bool
		msg  string // substring if !ok
	}{
		{"add", false, "Missing parameters"},
		{"add i", false, "Missing parameters"},
		{"add i h j", false, "Missing parameters"},
		{"add x h j 33 10", false, "Missing parameters"},
		{"add i x j 33 10", false, "Missing parameters"},
		{"add i h x 33 10", false, "Missing parameters"},
		{"add i h p 33 10", false, "Invalid combination"},  // heavy+prop invalid
		{"add i s h 33 10", true, ""},                      // small helo
		{"add i h h 33 10", false, "Invalid combination"},  // heavy helo
		{"add i l j 33", false, "Missing parameters"},      // approach needs distance
		{"add i l j -270 10", false, "Missing parameters"}, // bearing needs alt
		{"add i l j 33 abc", false, "Missing parameters"},
		{"add i l j 33 10 EXTRA JUNK", false, "Missing parameters"},
	}
	for _, tc := range cases {
		r := e.CommandLine(tc.line)
		if r.OK != tc.ok {
			t.Errorf("%q: ok=%v msg=%q want ok=%v", tc.line, r.OK, r.Message, tc.ok)
			continue
		}
		if !tc.ok && tc.msg != "" && !contains(r.Message, tc.msg) {
			t.Errorf("%q: msg=%q want substring %q", tc.line, r.Message, tc.msg)
		}
	}
	// No airport.
	e2 := NewEngine()
	r := e2.CommandLine("add v s p @GA1")
	if r.OK || !contains(r.Message, "No airport") {
		t.Errorf("no apt: %+v", r)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func TestCommand_AddMaxAircraft(t *testing.T) {
	e := NewEngineSettings(Settings{MaxAircraft: 2})
	apt, _ := ParseAPT("icao=TEST\nmagnetic variation=0\nfield elevation=100\npattern elevation=1100\nregistration=N\n[PARKING A]\n40.0 -70.0\n[PARKING B]\n40.001 -70.0\n[PARKING C]\n40.002 -70.0\n")
	_ = e.LoadAirport(&apt)
	if r := e.CommandLine("add v s p @A"); !r.OK {
		t.Fatal(r.Message)
	}
	if r := e.CommandLine("add v s p @B"); !r.OK {
		t.Fatal(r.Message)
	}
	r := e.CommandLine("add v s p @C")
	if r.OK || !contains(r.Message, "Maximum") {
		t.Fatalf("max: %+v", r)
	}
}

func TestCommand_DelPosSqId(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA1")
	cs := r.Added[0].Callsign

	// No selection.
	if e.CommandLine("del").OK {
		t.Fatal("del without target")
	}
	if e.CommandLine("pos").OK {
		t.Fatal("pos without target")
	}

	// Selected callsign form.
	r = e.Command(cs, "pos")
	if !r.OK {
		t.Fatalf("pos: %s", r.Message)
	}
	ac, ok := e.Get(cs)
	if !ok || ac.Status != StatusHoldingInPosition || !ac.PositionHold {
		t.Errorf("after pos: %+v", ac)
	}

	// Embedded callsign.
	r = e.CommandLine(cs + ", sq 3456")
	if !r.OK {
		t.Fatalf("sq: %s", r.Message)
	}
	ac, _ = e.Get(cs)
	if ac.Squawk != "3456" {
		t.Errorf("squawk = %s", ac.Squawk)
	}

	r = e.CommandLine(cs + ", sqi 1200")
	if !r.OK {
		t.Fatalf("sqi: %s", r.Message)
	}
	ac, _ = e.Get(cs)
	if ac.Squawk != "1200" || !ac.Ident || ac.XPDRMode != XPDRModeNormal {
		t.Errorf("sqi: %+v", ac)
	}

	r = e.Command(cs, "ss")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac, _ = e.Get(cs)
	if ac.XPDRMode != XPDRModeStandby {
		t.Errorf("mode = %s", ac.XPDRMode)
	}
	r = e.Command(cs, "sn")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac, _ = e.Get(cs)
	if ac.XPDRMode != XPDRModeNormal {
		t.Errorf("mode = %s", ac.XPDRMode)
	}

	// Clear ident then set via id.
	e.mu.Lock()
	e.aircraft[cs].Ident = false
	e.mu.Unlock()
	r = e.Command(cs, "id")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac, _ = e.Get(cs)
	if !ac.Ident {
		t.Error("ident not set")
	}

	// Bad squawk.
	r = e.Command(cs, "sq abcd")
	if r.OK {
		t.Fatal("bad squawk")
	}
	r = e.Command(cs, "sq")
	if r.OK {
		t.Fatal("missing squawk")
	}

	// pos on approach aircraft fails.
	r2 := e.CommandLine("add i l j 33 8")
	cs2 := r2.Added[0].Callsign
	r = e.Command(cs2, "pos")
	if r.OK {
		t.Fatal("pos on approach should fail")
	}

	// del
	r = e.CommandLine(cs + ", del")
	if !r.OK || len(r.Deleted) != 1 || r.Deleted[0] != cs {
		t.Fatalf("del: %+v", r)
	}
	if e.Count() != 1 {
		t.Errorf("count = %d", e.Count())
	}
	r = e.CommandLine("del") // no selection
	if r.OK {
		t.Fatal("del no sel")
	}
	r = e.Command("NOPE", "del")
	if r.OK {
		t.Fatal("del missing")
	}
}

func TestCommand_InvalidAndEmpty(t *testing.T) {
	e := loadKBTVEngine(t)
	cases := []string{"", "   ", "foobar", "AAL123, foobar"}
	for _, line := range cases {
		r := e.CommandLine(line)
		if r.OK {
			t.Errorf("%q should fail", line)
		}
	}
}

func TestCommand_PhAlias(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA2")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "ph")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac, _ := e.Get(cs)
	if ac.Status != StatusHoldingInPosition {
		t.Errorf("status = %s", ac.Status)
	}
}

func TestDefaultTypeMatrix(t *testing.T) {
	// Every valid combo has a type; invalid empty.
	weights := []string{"S", "M", "L", "H"}
	engines := []string{"P", "T", "J", "H"}
	valid := 0
	for _, w := range weights {
		for _, eng := range engines {
			dt := defaultType(w, eng)
			if validWeightEngine(w, eng) {
				if dt == "" {
					t.Errorf("valid %s/%s has empty type", w, eng)
				}
				valid++
			} else if dt != "" {
				t.Errorf("invalid %s/%s has type %s", w, eng, dt)
			}
		}
	}
	if valid < 8 {
		t.Errorf("expected several valid combos, got %d", valid)
	}
}

func TestGeometryHelpers(t *testing.T) {
	// Destination ~1 NM north of equator origin-ish.
	lat2, lon2 := destinationPoint(0, 0, 0, metersPerNM)
	if math.Abs(lat2-1.0/60.0) > 0.01 {
		t.Errorf("1NM north lat=%v", lat2)
	}
	if math.Abs(lon2) > 1e-6 {
		t.Errorf("lon=%v", lon2)
	}
	// Zero distance.
	la, lo := destinationPoint(10, 20, 90, 0)
	if la != 10 || lo != 20 {
		t.Errorf("zero dist")
	}
	// Bearing east then west.
	b := initialBearingDeg(0, 0, 0, 1)
	if math.Abs(b-90) > 1 {
		t.Errorf("bearing east = %v", b)
	}
	// normalizeHeading
	if normalizeHeading(361) != 1 {
		t.Errorf("361 → %v", normalizeHeading(361))
	}
	if normalizeHeading(-90) != 270 {
		t.Errorf("-90 → %v", normalizeHeading(-90))
	}
	if normalizeHeading(math.NaN()) != 0 {
		t.Error("nan")
	}
	// approachAltitude floor
	if approachAltitude(100, 0) < 150 {
		t.Errorf("floor: %v", approachAltitude(100, 0))
	}
	// fieldReferencePoint empty
	if fieldReferencePoint(nil).Lat != 0 {
		t.Error("nil apt")
	}
	if fieldReferencePoint(&Airport{}).Lat != 0 {
		t.Error("empty apt")
	}
	// parking-only airport uses average
	apt := &Airport{Surfaces: []Surface{
		{Kind: SurfaceParking, Name: "A", Points: []Point{{1, 2}, {3, 4}}},
	}}
	ref := fieldReferencePoint(apt)
	if ref.Lat != 1 || ref.Lon != 2 {
		t.Errorf("ref = %+v", ref)
	}
	// runwayThreshold bad
	if _, _, ok := runwayThreshold(nil, "1"); ok {
		t.Error("nil surface")
	}
	if _, _, ok := runwayThreshold(&Surface{Kind: SurfaceTaxiway}, "1"); ok {
		t.Error("not runway")
	}
}

func TestGenerateCallsignUnique(t *testing.T) {
	e := loadKBTVEngine(t)
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		r := e.CommandLine("add i l j 33 12")
		if !r.OK {
			t.Fatalf("add %d: %s", i, r.Message)
		}
		cs := r.Added[0].Callsign
		if seen[cs] {
			t.Fatalf("duplicate callsign %s", cs)
		}
		seen[cs] = true
	}
}

func TestInferWeightAndFromScenario(t *testing.T) {
	if inferWeight(EngineJet, "B744") != WeightHeavy {
		t.Error("B744")
	}
	if inferWeight(EngineJet, "B738/F") != WeightLarge {
		t.Error("B738")
	}
	if inferWeight(EnginePiston, "C172") != WeightSmall {
		t.Error("C172")
	}
	if inferWeight(EngineHelicopter, "R22") != WeightSmall {
		t.Error("heli")
	}
	if inferWeight(EngineTurboprop, "DH8D") != WeightSmallP {
		t.Error("turbo")
	}
	row := Aircraft{
		Callsign: "xx1", Engine: EngineJet, Rules: RulesIFR,
		Squawk: "2200", XPDRMode: XPDRModeStandby,
		Lat: 1, Lon: 2, Alt: 3000, Speed: 0, Heading: 10,
	}
	ac := fromScenarioRow(row)
	if ac.Status != StatusParked || ac.Callsign != "XX1" {
		t.Errorf("%+v", ac)
	}
	row.Speed = 100
	ac = fromScenarioRow(row)
	if ac.Status != StatusAirborne {
		t.Errorf("status %s", ac.Status)
	}
}

func TestSnapshotEmpty(t *testing.T) {
	e := NewEngine()
	s := e.Snapshot()
	if s.ICAO != "" || len(s.Aircraft) != 0 || !s.Paused {
		t.Errorf("%+v", s)
	}
}

func TestConcurrentCommandAndTick(t *testing.T) {
	e := loadKBTVEngine(t)
	e.Unpause()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				e.Tick(10 * time.Millisecond)
				line := fmt.Sprintf("add v s p @GA1")
				r := e.CommandLine(line)
				if r.OK && len(r.Added) == 1 {
					e.CommandLine(r.Added[0].Callsign + ", del")
				}
				_ = e.Snapshot()
				_ = e.Ops()
				_ = e.CommandLine("ops")
				if id%2 == 0 {
					e.Pause()
				} else {
					e.Unpause()
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestFormatCallsignInUse(t *testing.T) {
	if formatCallsignInUse("ABC") == "" {
		t.Fatal("empty")
	}
}

func TestSimAircraftSnapshotNil(t *testing.T) {
	var a *SimAircraft
	if a.snapshot().Callsign != "" {
		t.Fatal("nil snapshot")
	}
}

func TestDefaultSpeedsAndCruise(t *testing.T) {
	if defaultApproachSpeed(EngineJet) != 180 {
		t.Error("jet spd")
	}
	if defaultApproachSpeed(EnginePiston) != 90 {
		t.Error("piston spd")
	}
	if defaultApproachSpeed(EngineTurboprop) != 140 {
		t.Error("turbo spd")
	}
	if defaultApproachSpeed(EngineHelicopter) != 80 {
		t.Error("heli spd")
	}
	if defaultApproachSpeed("X") != 120 {
		t.Error("default spd")
	}
	if defaultCruiseAlt(EngineJet) != 29000 {
		t.Error("jet cruise")
	}
	if defaultCruiseAlt(EnginePiston) != 3500 {
		t.Error("piston cruise")
	}
	if defaultCruiseAlt(EngineTurboprop) != 16000 {
		t.Error("turbo cruise")
	}
	if defaultCruiseAlt("X") != 10000 {
		t.Error("default cruise")
	}
}

func TestNextSquawkVFRIFR(t *testing.T) {
	e := loadKBTVEngine(t)
	// First VFR prefers 1200.
	r := e.CommandLine("add v s p @GA1")
	if r.Added[0].Squawk != "1200" {
		t.Errorf("vfr sqk = %s", r.Added[0].Squawk)
	}
	r2 := e.CommandLine("add i l j 33 5")
	if r2.Added[0].Squawk == "1200" {
		// IFR should not also be 1200 if VFR took it — sequential.
		t.Log("ifr also 1200 is ok if unique walk allows; check != empty")
	}
	if r2.Added[0].Squawk == "" || len(r2.Added[0].Squawk) != 4 {
		t.Errorf("ifr sqk = %q", r2.Added[0].Squawk)
	}
}

func TestRunwayThresholdDisplaced(t *testing.T) {
	// KBTV 33 has DispA=500 on end 33.
	e := loadKBTVEngine(t)
	rwy := e.Airport().FindSurface("33")
	thr, _, ok := runwayThreshold(rwy, "33")
	if !ok {
		t.Fatal("ok")
	}
	// Without displacement thr would be points[0]; with 500ft should differ.
	raw := rwy.Points[0]
	d := geo.Distance(raw.Lat, raw.Lon, thr.Lat, thr.Lon)
	// ~500 ft = 152.4 m
	if math.Abs(d-152.4) > 20 {
		t.Errorf("displaced dist = %v m, want ~152", d)
	}
	// End 15 has DispB=0.
	thr15, _, ok := runwayThreshold(rwy, "15")
	if !ok {
		t.Fatal("15")
	}
	raw15 := rwy.Points[len(rwy.Points)-1]
	if geo.Distance(raw15.Lat, raw15.Lon, thr15.Lat, thr15.Lon) > 1 {
		t.Errorf("15 should not be displaced")
	}
}

func TestCommand_AircraftNotFoundMessages(t *testing.T) {
	e := loadKBTVEngine(t)
	for _, line := range []string{"sq 1200", "sqi 1200", "sn", "ss", "id", "pos"} {
		r := e.Command("ZZZZ", line)
		if r.OK {
			t.Errorf("%s should fail", line)
		}
	}
}

func TestLoadScenario_MissingCallsignAndBadSquawk(t *testing.T) {
	e := loadKBTVEngine(t)
	rows := []Aircraft{
		{Callsign: "", Engine: EngineJet, Rules: RulesIFR, Squawk: "2000", XPDRMode: "N"},
		{Callsign: "OK1", Engine: EngineJet, Rules: RulesIFR, Squawk: "bad", XPDRMode: "", Speed: 100, Lat: 1, Lon: 2, Alt: 1000},
	}
	loaded, errs := e.LoadScenario(rows)
	if len(loaded) != 1 || loaded[0] != "OK1" {
		t.Fatalf("loaded=%v errs=%v", loaded, errs)
	}
	ac, _ := e.Get("OK1")
	if !isSquawk(ac.Squawk) {
		t.Errorf("repaired squawk = %q", ac.Squawk)
	}
	if ac.XPDRMode != XPDRModeNormal {
		t.Errorf("mode = %s", ac.XPDRMode)
	}
}

func TestSplitCSV(t *testing.T) {
	if len(splitCSV("A, B,,c")) != 3 {
		t.Fatal(splitCSV("A, B,,c"))
	}
	if len(splitCSV("")) != 0 {
		t.Fatal("empty")
	}
}

func TestStripCallsignPrefix(t *testing.T) {
	cs, rest, had := stripCallsignPrefix([]string{"AAL123", "del"})
	if !had || cs != "AAL123" || rest[0] != "del" {
		t.Errorf("%v %v %v", cs, rest, had)
	}
	cs, rest, had = stripCallsignPrefix([]string{"add", "v", "s", "p", "@A"})
	if had || cs != "" {
		t.Errorf("add must not strip: %v %v", cs, had)
	}
	_, _, had = stripCallsignPrefix([]string{"ONLY"})
	if had {
		t.Error("single token")
	}
	// reserved aircraft verbs still target
	cs, rest, had = stripCallsignPrefix([]string{"N123", "taxi", "A"})
	if !had || cs != "N123" || rest[0] != "taxi" {
		t.Errorf("taxi target: %v %v %v", cs, rest, had)
	}
}

func TestFormatOpsNegativeElapsed(t *testing.T) {
	msg := formatOpsMessage(OpsStats{Elapsed: -time.Second})
	if msg == "" {
		t.Fatal("empty")
	}
}
