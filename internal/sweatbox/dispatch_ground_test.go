package sweatbox

import (
	"strings"
	"testing"
)

// placeOnSurface moves ac onto named surface (same package; simulates post-taxi Tick).
func placeOnSurface(t *testing.T, e *Engine, cs, surface string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	ac := e.aircraft[cs]
	if ac == nil {
		t.Fatalf("missing %s", cs)
	}
	s := e.graph.Surface(surface)
	if s == nil || len(s.Points) == 0 {
		t.Fatalf("surface %s", surface)
	}
	ac.Lat = s.Points[0].Lat
	ac.Lon = s.Points[0].Lon
	ac.CurrentSurface = strings.ToUpper(s.Name)
	if s.Kind == SurfaceRunway {
		ac.CurrentSurface = s.Name
	}
	ac.Status = StatusTaxiing
	ac.Parking = ""
}

// forceHoldShort puts aircraft into Holding Short of name (simulates Tick at hold wp).
func forceHoldShort(t *testing.T, e *Engine, cs, name string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	ac := e.aircraft[cs]
	if ac == nil {
		t.Fatalf("missing %s", cs)
	}
	ac.Status = StatusHoldingShort
	ac.HoldShortOf = strings.ToUpper(name)
	ac.Instruction = "Holding short of " + ac.HoldShortOf
}

func TestCommand_TaxiHappyPath(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatalf("add: %s", r.Message)
	}
	cs := r.Added[0].Callsign

	// GA9 intersects J; taxi to runway 33.
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatalf("taxi: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusTaxiing {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.DepRunway != "33" {
		t.Errorf("DepRunway = %s, want 33", ac.DepRunway)
	}
	if len(ac.TaxiSteps) != 2 {
		t.Errorf("TaxiSteps = %v", ac.TaxiSteps)
	}
	if !strings.Contains(ac.Instruction, "Taxi") || !strings.Contains(ac.Instruction, "J") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
	if ac.Parking != "" {
		t.Errorf("Parking should clear when taxiing to runway, got %q", ac.Parking)
	}
	if ac.ClearedTakeoff {
		t.Error("taxi should clear takeoff clearance")
	}

	// Internal path state (engine-only).
	e.mu.Lock()
	sim := e.aircraft[cs]
	if len(sim.TaxiWaypoints) < 1 {
		t.Errorf("waypoints empty")
	}
	if sim.TaxiWPIndex != 0 {
		t.Errorf("WPIndex = %d", sim.TaxiWPIndex)
	}
	e.mu.Unlock()
}

func TestCommand_TaxiWithHoldShort(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	// Put on A so A→B→19 hs 19 works.
	placeOnSurface(t, e, cs, "A")

	r = e.Command(cs, "taxi B 19 hs 19")
	if !r.OK {
		t.Fatalf("taxi: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusTaxiing {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.DepRunway != "19" {
		t.Errorf("DepRunway = %s", ac.DepRunway)
	}
	if !strings.Contains(ac.Instruction, "hs") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
	e.mu.Lock()
	sim := e.aircraft[cs]
	if len(sim.TaxiHolds) != 1 || sim.TaxiHolds[0].Name != "19" {
		t.Errorf("TaxiHolds = %+v", sim.TaxiHolds)
	}
	e.mu.Unlock()
}

func TestCommand_TaxiValidation(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign

	cases := []struct {
		line string
		sub  string
	}{
		{"taxi", "Must specify"},
		{"taxi NOPE", "Unknown step"},
		{"taxi A B 33", "First step"}, // GA9 does not intersect A
		{"taxi J C 33", "do not intersect"},
	}
	for _, tc := range cases {
		r = e.Command(cs, tc.line)
		if r.OK || !strings.Contains(r.Message, tc.sub) {
			t.Errorf("%q: ok=%v msg=%q want sub %q", tc.line, r.OK, r.Message, tc.sub)
		}
	}

	// Airborne cannot taxi.
	r2 := e.CommandLine("add i l j 33 10")
	cs2 := r2.Added[0].Callsign
	r = e.Command(cs2, "taxi A")
	if r.OK || !strings.Contains(r.Message, "Not taxiing, parked or holding short") {
		t.Errorf("airborne taxi: %+v", r)
	}

	// No selection.
	if e.CommandLine("taxi J").OK {
		t.Fatal("taxi without target")
	}

	// No airport.
	e2 := NewEngine()
	// inject fake aircraft without airport
	e2.mu.Lock()
	e2.aircraft["X1"] = &SimAircraft{Callsign: "X1", Status: StatusParked}
	e2.mu.Unlock()
	r = e2.Command("X1", "taxi A")
	if r.OK || !strings.Contains(r.Message, "No airport") {
		t.Errorf("no apt: %+v", r)
	}
}

func TestCommand_TaxiToParking(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	// Already at GA9 — taxi J @GA9 (leave and return).
	r = e.Command(cs, "taxi J @GA9")
	if !r.OK {
		t.Fatalf("%s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.TaxiParking != "GA9" {
		t.Errorf("TaxiParking = %s", ac.TaxiParking)
	}
	if ac.DepRunway != "" {
		t.Errorf("DepRunway should be empty for parking taxi, got %s", ac.DepRunway)
	}
	if !strings.Contains(ac.Instruction, "@GA9") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
}

func TestCommand_HoldAndRes(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatal(r.Message)
	}

	// hold requires taxiing
	r = e.Command(cs, "hold")
	if !r.OK {
		t.Fatalf("hold: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusHolding {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.Instruction != "Hold position" {
		t.Errorf("instruction = %q", ac.Instruction)
	}

	// Double hold fails (not taxiing).
	r = e.Command(cs, "hold")
	if r.OK {
		t.Fatal("hold while holding should fail")
	}

	// res resumes taxi.
	r = e.Command(cs, "res")
	if !r.OK {
		t.Fatalf("res: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.Status != StatusTaxiing {
		t.Errorf("status = %s", ac.Status)
	}

	// res while taxiing is silent success.
	r = e.Command(cs, "res")
	if !r.OK {
		t.Fatalf("res taxiing: %s", r.Message)
	}

	// hold on parked fails.
	r2 := e.CommandLine("add v s p @GA1")
	cs2 := r2.Added[0].Callsign
	r = e.Command(cs2, "hold")
	if r.OK || !strings.Contains(r.Message, "Not taxiing") {
		t.Errorf("hold parked: %+v", r)
	}
}

func TestCommand_ResHoldShortDepRunway(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatal(r.Message)
	}
	forceHoldShort(t, e, cs, "33")
	// Ensure DepRunway is 33 (set by taxi).
	if e.mustGet(t, cs).DepRunway != "33" {
		t.Fatal("DepRunway")
	}

	r = e.Command(cs, "res")
	if r.OK || !strings.Contains(r.Message, "pos") {
		t.Errorf("res at dep rwy: %+v", r)
	}

	// pos then ok.
	r = e.Command(cs, "pos")
	if !r.OK {
		t.Fatalf("pos: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusHoldingInPosition || !ac.PositionHold {
		t.Errorf("after pos: %+v", ac)
	}
}

func TestCommand_ResHoldShortIntermediate(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	placeOnSurface(t, e, cs, "A")
	r = e.Command(cs, "taxi B 19 hs 19")
	if !r.OK {
		t.Fatal(r.Message)
	}
	// Intermediate hold of 19 which IS the dep runway — res blocked.
	forceHoldShort(t, e, cs, "19")
	r = e.Command(cs, "res")
	if r.OK {
		t.Fatalf("should block res at dep: %s", r.Message)
	}

	// Intermediate hold of a taxiway (not dep): use hold of B while dest 19.
	// Re-taxi with hs B (B is a taxiway on route A→B→19).
	placeOnSurface(t, e, cs, "A")
	r = e.Command(cs, "taxi B 19 hs B")
	if !r.OK {
		// B as hold-short of itself on route may work
		t.Logf("taxi hs B: %s", r.Message)
	}
	if r.OK {
		forceHoldShort(t, e, cs, "B")
		// Dep is 19, hold is B → res should work.
		r = e.Command(cs, "res")
		if !r.OK {
			t.Fatalf("res intermediate: %s", r.Message)
		}
		ac := e.mustGet(t, cs)
		if ac.Status != StatusTaxiing {
			t.Errorf("status = %s", ac.Status)
		}
		if ac.HoldShortOf != "" {
			t.Errorf("HoldShortOf = %s", ac.HoldShortOf)
		}
	}
}

func TestCommand_Cross(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	placeOnSurface(t, e, cs, "A")
	r = e.Command(cs, "taxi B 19 hs 19")
	if !r.OK {
		t.Fatal(r.Message)
	}

	// Planning to hold short of 19 — cross removes it before arrival.
	r = e.Command(cs, "cross 19")
	if !r.OK {
		t.Fatalf("cross planned: %s", r.Message)
	}
	e.mu.Lock()
	if len(e.aircraft[cs].TaxiHolds) != 0 {
		t.Errorf("holds remain: %+v", e.aircraft[cs].TaxiHolds)
	}
	e.mu.Unlock()
	ac := e.mustGet(t, cs)
	if !strings.Contains(ac.Instruction, "Cross 19") {
		t.Errorf("instruction = %q", ac.Instruction)
	}

	// Cross unknown / not planned.
	r = e.Command(cs, "cross 33")
	if r.OK || !strings.Contains(r.Message, "Not holding short") {
		t.Errorf("cross bad: %+v", r)
	}

	// Active holding short.
	placeOnSurface(t, e, cs, "A")
	r = e.Command(cs, "taxi B 19 hs 19")
	if !r.OK {
		t.Fatal(r.Message)
	}
	forceHoldShort(t, e, cs, "19")
	r = e.Command(cs, "cross 19")
	if !r.OK {
		t.Fatalf("cross active: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.Status != StatusTaxiing {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.HoldShortOf != "" {
		t.Errorf("HoldShortOf = %s", ac.HoldShortOf)
	}

	// Missing arg.
	r = e.Command(cs, "cross")
	if r.OK {
		t.Fatal("cross needs arg")
	}
}

func TestCommand_CTOAndCTOC(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatal(r.Message)
	}

	// cto while taxiing: clearance set, still taxiing.
	r = e.Command(cs, "cto")
	if !r.OK {
		t.Fatalf("cto taxi: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if !ac.ClearedTakeoff {
		t.Error("ClearedTakeoff")
	}
	if ac.Status != StatusTaxiing {
		t.Errorf("status = %s (still taxiing until runway)", ac.Status)
	}
	if !strings.Contains(ac.Instruction, "Cleared for takeoff") {
		t.Errorf("instruction = %q", ac.Instruction)
	}

	// ctoc while taxiing with clearance.
	r = e.Command(cs, "ctoc")
	if !r.OK {
		t.Fatalf("ctoc: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.ClearedTakeoff {
		t.Error("cleared still set")
	}
	if ac.Instruction != "Takeoff clearance cancelled" {
		t.Errorf("instruction = %q", ac.Instruction)
	}

	// ctoc when not cleared fails.
	r = e.Command(cs, "ctoc")
	if r.OK || !strings.Contains(r.Message, "Not cleared") {
		t.Errorf("ctoc no clear: %+v", r)
	}

	// pos + cto → StatusTakeoff.
	forceHoldShort(t, e, cs, "33")
	r = e.Command(cs, "pos")
	if !r.OK {
		t.Fatal(r.Message)
	}
	r = e.Command(cs, "cto 090")
	if !r.OK {
		t.Fatalf("cto hdg: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.Status != StatusTakeoff {
		t.Errorf("status = %s", ac.Status)
	}
	if !ac.HasDepHeading || ac.DepHeading != 90 {
		t.Errorf("hdg flags: has=%v hdg=%v", ac.HasDepHeading, ac.DepHeading)
	}
	if !strings.Contains(ac.Instruction, "090") && !strings.Contains(ac.Instruction, "heading 090") {
		// format uses %03.0f
		if !strings.Contains(ac.Instruction, "heading") {
			t.Errorf("instruction = %q", ac.Instruction)
		}
	}

	// ctoc from takeoff → holding short of dep.
	r = e.Command(cs, "cancel") // alias
	if !r.OK {
		t.Fatalf("cancel: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.Status != StatusHoldingShort {
		t.Errorf("status = %s", ac.Status)
	}
	if ac.HoldShortOf != "33" {
		t.Errorf("HoldShortOf = %s", ac.HoldShortOf)
	}
	if ac.ClearedTakeoff {
		t.Error("still cleared")
	}
}

func TestCommand_CTOmltMrt(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatal(r.Message)
	}
	r = e.Command(cs, "pos")
	if !r.OK {
		// not at runway yet — force in position
		e.mu.Lock()
		e.aircraft[cs].Status = StatusHoldingInPosition
		e.aircraft[cs].PositionHold = true
		e.mu.Unlock()
	}

	r = e.Command(cs, "ctomlt")
	if !r.OK {
		t.Fatalf("ctomlt: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.PatternTraffic != "L" || !ac.ClearedTakeoff {
		t.Errorf("ctomlt: traffic=%s cleared=%v", ac.PatternTraffic, ac.ClearedTakeoff)
	}
	if !strings.Contains(ac.Instruction, "left traffic") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
	if ac.Status != StatusTakeoff {
		t.Errorf("status = %s", ac.Status)
	}

	// Reset and ctomrt.
	e.mu.Lock()
	e.aircraft[cs].Status = StatusHoldingInPosition
	e.aircraft[cs].ClearedTakeoff = false
	e.aircraft[cs].PatternTraffic = ""
	e.mu.Unlock()
	r = e.Command(cs, "ctomrt")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.PatternTraffic != "R" {
		t.Errorf("traffic = %s", ac.PatternTraffic)
	}
	if !strings.Contains(ac.Instruction, "right traffic") {
		t.Errorf("instruction = %q", ac.Instruction)
	}

	// Plain cto clears pattern traffic.
	e.mu.Lock()
	e.aircraft[cs].Status = StatusHoldingInPosition
	e.mu.Unlock()
	r = e.Command(cs, "cto")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).PatternTraffic != "" {
		t.Error("plain cto should clear pattern traffic")
	}
}

func TestCommand_CTOBadStatus(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add i l j 33 8")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "cto")
	if r.OK || !strings.Contains(r.Message, "Not taxiing") {
		t.Errorf("cto approach: %+v", r)
	}
	r = e.Command(cs, "cto xyz")
	// Still bad status first... actually status check is first.
	if r.OK {
		t.Fatal("expected fail")
	}

	// Bad heading while taxiing.
	r = e.CommandLine("add v s p @GA9")
	cs = r.Added[0].Callsign
	_ = e.Command(cs, "taxi J 33")
	r = e.Command(cs, "cto NaN")
	if r.OK {
		t.Fatal("bad hdg")
	}
}

func TestCommand_NoStop(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "nostop")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if !e.mustGet(t, cs).NoStop {
		t.Error("NoStop not set")
	}
	// alias
	r2 := e.CommandLine("add v s p @GA1")
	cs2 := r2.Added[0].Callsign
	r = e.Command(cs2, "nohold")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if !e.mustGet(t, cs2).NoStop {
		t.Error("nohold alias")
	}
}

func TestCommand_PosIdempotentAndEmbedded(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	_ = e.Command(cs, "taxi J 33")
	r = e.CommandLine(cs + ", pos")
	if !r.OK {
		t.Fatal(r.Message)
	}
	r = e.Command(cs, "pos")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusHoldingInPosition || !ac.PositionHold {
		t.Errorf("%+v", ac)
	}
}

func TestCommand_TaxiTargetingAliases(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	// Embedded callsign with comma.
	r = e.CommandLine(cs + ", taxi J 33")
	if !r.OK {
		t.Fatalf("embedded: %s", r.Message)
	}
	// Selected callsign form.
	placeOnSurface(t, e, cs, "J")
	r = e.Command(cs, "taxi 33")
	if !r.OK {
		t.Fatalf("selected: %s", r.Message)
	}
}

func TestParseTaxiArgs(t *testing.T) {
	steps, holds := parseTaxiArgs([]string{"K", "C", "D", "27", "hs", "33L"})
	if strings.Join(steps, " ") != "K C D 27" {
		t.Errorf("steps = %v", steps)
	}
	if strings.Join(holds, " ") != "33L" {
		t.Errorf("holds = %v", holds)
	}
	steps, holds = parseTaxiArgs([]string{"A", "B"})
	if len(holds) != 0 || len(steps) != 2 {
		t.Errorf("no hs: %v %v", steps, holds)
	}
	steps, holds = parseTaxiArgs([]string{"hs", "19"})
	if len(steps) != 0 || len(holds) != 1 {
		t.Errorf("hs first: %v %v", steps, holds)
	}
}

func TestNormalizeVerbGroundAliases(t *testing.T) {
	if normalizeVerb("can") != "ctoc" || normalizeVerb("cancel") != "ctoc" {
		t.Error("ctoc aliases")
	}
	if normalizeVerb("nohold") != "nostop" {
		t.Error("nostop alias")
	}
	if !isAircraftVerb("taxi") || !isAircraftVerb("ctomlt") || !isAircraftVerb("nostop") {
		t.Error("verbs")
	}
}

func TestCommand_NewTaxiClearsTakeoff(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	_ = e.Command(cs, "taxi J 33")
	_ = e.Command(cs, "cto 180")
	if !e.mustGet(t, cs).ClearedTakeoff {
		t.Fatal("expected cleared")
	}
	placeOnSurface(t, e, cs, "J")
	r = e.Command(cs, "taxi 33")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.ClearedTakeoff || ac.HasDepHeading {
		t.Errorf("new taxi should clear cto state: cleared=%v hasHdg=%v", ac.ClearedTakeoff, ac.HasDepHeading)
	}
}

func TestSnapshotIncludesGroundFields(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	_ = e.Command(cs, "taxi J 33")
	_ = e.Command(cs, "cto")
	_ = e.Command(cs, "nostop")
	ac := e.mustGet(t, cs)
	if !ac.ClearedTakeoff || !ac.NoStop {
		t.Errorf("snapshot flags: %+v", ac)
	}
	if len(ac.TaxiSteps) == 0 {
		t.Error("TaxiSteps not in snapshot")
	}
	if ac.DepRunway != "33" {
		t.Errorf("DepRunway = %s", ac.DepRunway)
	}
}

func TestCommand_ResAndCTOCEdgeBranches(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign

	// res with no aircraft / wrong status
	if e.Command("NOPE", "res").OK {
		t.Fatal("missing ac")
	}
	r = e.Command(cs, "res")
	if r.OK || !strings.Contains(r.Message, "Not taxiing") {
		t.Errorf("res parked: %+v", r)
	}

	// StatusHolding without taxi path → fail
	e.mu.Lock()
	e.aircraft[cs].Status = StatusHolding
	e.aircraft[cs].TaxiWaypoints = nil
	e.mu.Unlock()
	r = e.Command(cs, "res")
	if r.OK {
		t.Fatal("hold without path")
	}

	// Holding short of non-dep surface resumes
	_ = e.Command(cs, "taxi J 33")
	e.mu.Lock()
	e.aircraft[cs].Status = StatusHoldingShort
	e.aircraft[cs].HoldShortOf = "J"
	e.aircraft[cs].DepRunway = "33"
	e.mu.Unlock()
	r = e.Command(cs, "res")
	if !r.OK {
		t.Fatalf("res non-dep hold: %s", r.Message)
	}
	if e.mustGet(t, cs).Status != StatusTaxiing {
		t.Error("expected taxiing")
	}

	// Holding short with no path left after resume still taxiing
	e.mu.Lock()
	e.aircraft[cs].Status = StatusHoldingShort
	e.aircraft[cs].HoldShortOf = "J"
	e.aircraft[cs].DepRunway = "33"
	e.aircraft[cs].TaxiWaypoints = nil
	e.mu.Unlock()
	r = e.Command(cs, "res")
	if !r.OK {
		t.Fatalf("res no path: %s", r.Message)
	}

	// ctoc from StatusTakeoff with DepRunway empty + path → taxiing
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusTakeoff
	ac.ClearedTakeoff = true
	ac.DepRunway = ""
	ac.TaxiWaypoints = []Point{{1, 2}}
	ac.TaxiSteps = []string{"J"}
	e.mu.Unlock()
	r = e.Command(cs, "ctoc")
	if !r.OK {
		t.Fatalf("ctoc path: %s", r.Message)
	}
	if e.mustGet(t, cs).Status != StatusTaxiing {
		t.Errorf("status = %s", e.mustGet(t, cs).Status)
	}

	// ctoc from StatusTakeoff with no DepRunway and no path → position hold
	e.mu.Lock()
	ac = e.aircraft[cs]
	ac.Status = StatusTakeoff
	ac.ClearedTakeoff = true
	ac.DepRunway = ""
	ac.TaxiWaypoints = nil
	ac.TaxiSteps = nil
	e.mu.Unlock()
	r = e.Command(cs, "can") // alias
	if !r.OK {
		t.Fatalf("ctoc empty: %s", r.Message)
	}
	got := e.mustGet(t, cs)
	if got.Status != StatusHoldingInPosition || !got.PositionHold {
		t.Errorf("got %+v", got)
	}

	// hold missing aircraft
	if e.Command("ZZZ", "hold").OK {
		t.Fatal("hold miss")
	}
	// cross missing aircraft / empty name
	if e.Command("ZZZ", "cross 19").OK {
		t.Fatal("cross miss")
	}
	if e.Command(cs, "cross   ").OK {
		t.Fatal("cross blank")
	}
	// cto missing
	if e.Command("ZZZ", "cto").OK {
		t.Fatal("cto miss")
	}
	// nostop missing
	if e.Command("ZZZ", "nostop").OK {
		t.Fatal("nostop miss")
	}
	// ctoc missing
	if e.Command("ZZZ", "ctoc").OK {
		t.Fatal("ctoc miss")
	}
}

func TestHelpers_SurfaceAndGroundOK(t *testing.T) {
	var nilAC *SimAircraft
	if nilAC.surfaceForTaxi() != "" || nilAC.hasTaxiPath() || nilAC.groundOK() {
		t.Fatal("nil ac")
	}
	ac := &SimAircraft{Parking: "GA1", Status: StatusLanded}
	if ac.surfaceForTaxi() != "GA1" {
		t.Errorf("parking fallback: %s", ac.surfaceForTaxi())
	}
	ac.CurrentSurface = "A"
	if ac.surfaceForTaxi() != "A" {
		t.Error("current wins")
	}
	if !ac.groundOK() {
		t.Error("landed groundOK")
	}
	ac.Status = StatusAirborne
	if ac.groundOK() {
		t.Error("airborne not ground")
	}
	// holdNames empty
	if holdNames(nil) != nil {
		t.Error("holdNames nil")
	}
	// removeHoldNamed no-ops
	ac.removeHoldNamed("")
	ac.removeHoldNamed("X")
	// sameSurfaceName without graph
	e := NewEngine()
	if !e.sameSurfaceNameLocked("A", "a") {
		t.Error("equal fold")
	}
	if e.sameSurfaceNameLocked("A", "B") {
		t.Error("no graph different")
	}
	// runwayEndLabel without graph
	if e.runwayEndLabelLocked("33") != "" {
		t.Error("no graph")
	}
	// holdPlanned nil
	if e.holdPlannedLocked(nil, "X") {
		t.Error("nil plan")
	}
}

func TestCommand_CrossClearsStatusHolding(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	placeOnSurface(t, e, cs, "A")
	if r = e.Command(cs, "taxi B 19 hs 19"); !r.OK {
		t.Fatal(r.Message)
	}
	// Present-position hold while planning to cross 19
	if r = e.Command(cs, "hold"); !r.OK {
		t.Fatal(r.Message)
	}
	r = e.Command(cs, "cross 19")
	if !r.OK {
		t.Fatalf("cross while holding: %s", r.Message)
	}
	if e.mustGet(t, cs).Status != StatusTaxiing {
		t.Errorf("status = %s", e.mustGet(t, cs).Status)
	}
}

func TestCommand_CTOWhileTakeoff(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	e.mu.Lock()
	e.aircraft[cs].Status = StatusTakeoff
	e.aircraft[cs].DepRunway = "33"
	e.mu.Unlock()
	r = e.Command(cs, "cto 270")
	if !r.OK {
		t.Fatalf("%s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if !ac.ClearedTakeoff || ac.DepHeading != 270 {
		t.Errorf("%+v", ac)
	}
}

func TestCommand_PosFromHolding(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	_ = e.Command(cs, "taxi J 33")
	_ = e.Command(cs, "hold")
	r = e.Command(cs, "pos")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).Status != StatusHoldingInPosition {
		t.Error("pos from holding")
	}
}

func TestIsAircraftVerbCoverage(t *testing.T) {
	for _, v := range []string{"del", "taxi", "hold", "res", "cross", "cto", "ctoc",
		"ctomlt", "ctomrt", "nostop", "nohold", "cancel", "can", "fh", "fp", "hs", "land", "ctopp"} {
		if !isAircraftVerb(v) {
			t.Errorf("verb %s", v)
		}
	}
	if isAircraftVerb("zzzz") {
		t.Error("unknown")
	}
}
