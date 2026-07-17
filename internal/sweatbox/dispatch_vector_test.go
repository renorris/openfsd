package sweatbox

import (
	"math"
	"strings"
	"testing"
	"time"
)

// addAirborne places an IFR jet on approach and returns its callsign.
func addAirborne(t *testing.T, e *Engine) string {
	t.Helper()
	r := e.CommandLine("add i l j 33 10")
	if !r.OK {
		t.Fatalf("add airborne: %s", r.Message)
	}
	return r.Added[0].Callsign
}

func TestCommand_FlyHeading(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	before, _ := e.Get(cs)
	origHdg := before.Heading

	r := e.Command(cs, "fh 120")
	if !r.OK {
		t.Fatalf("fh: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if !ac.HasDesiredHeading || math.Abs(ac.DesiredHeading-120) > 1e-9 {
		t.Errorf("DesiredHeading = %v has=%v", ac.DesiredHeading, ac.HasDesiredHeading)
	}
	if ac.TurnDir != TurnShortest {
		t.Errorf("TurnDir = %d", ac.TurnDir)
	}
	if ac.ImmediateHeading {
		t.Error("fh must not set ImmediateHeading")
	}
	// fh does not snap heading (kinematics in PR 4).
	if math.Abs(ac.Heading-origHdg) > 1e-9 {
		t.Errorf("fh must not change Heading: got %v want %v", ac.Heading, origHdg)
	}
	if !strings.Contains(ac.Instruction, "120") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	// Missing / invalid.
	for _, line := range []string{"fh", "fh NaN", "fh +Inf"} {
		r = e.Command(cs, line)
		if r.OK {
			t.Errorf("%q: expected failure", line)
		}
	}
	// No selection.
	if e.CommandLine("fh 90").OK {
		t.Error("fh without target should fail")
	}
}

func TestCommand_FlyHeadingNow(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	r := e.CommandLine(cs + ", fhn 270")
	if !r.OK {
		t.Fatalf("fhn: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if !ac.HasDesiredHeading || math.Abs(ac.DesiredHeading-270) > 1e-9 {
		t.Errorf("DesiredHeading = %v", ac.DesiredHeading)
	}
	if !ac.ImmediateHeading {
		t.Error("fhn must set ImmediateHeading")
	}
	if math.Abs(ac.Heading-270) > 1e-9 {
		t.Errorf("fhn must snap Heading: got %v", ac.Heading)
	}
}

func TestCommand_TurnLeftRight(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	r := e.Command(cs, "tr 60")
	if !r.OK {
		t.Fatalf("tr: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.TurnDir != TurnRight || math.Abs(ac.DesiredHeading-60) > 1e-9 {
		t.Errorf("tr: hdg=%v turn=%d", ac.DesiredHeading, ac.TurnDir)
	}
	if !strings.Contains(strings.ToLower(ac.Instruction), "right") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	r = e.Command(cs, "tl 350")
	if !r.OK {
		t.Fatalf("tl: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.TurnDir != TurnLeft || math.Abs(ac.DesiredHeading-350) > 1e-9 {
		t.Errorf("tl: hdg=%v turn=%d", ac.DesiredHeading, ac.TurnDir)
	}
	if !strings.Contains(strings.ToLower(ac.Instruction), "left") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	// Heading normalization: 720 → 0, -90 → 270.
	r = e.Command(cs, "fh 720")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if math.Abs(e.mustGet(t, cs).DesiredHeading-0) > 1e-9 {
		t.Errorf("720 → %v", e.mustGet(t, cs).DesiredHeading)
	}
	r = e.Command(cs, "fh -90")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if math.Abs(e.mustGet(t, cs).DesiredHeading-270) > 1e-9 {
		t.Errorf("-90 → %v", e.mustGet(t, cs).DesiredHeading)
	}
}

func TestCommand_FlyPresentHeading(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Snap to a known heading first.
	if r := e.Command(cs, "fhn 045"); !r.OK {
		t.Fatal(r.Message)
	}

	r := e.Command(cs, "fph")
	if !r.OK {
		t.Fatalf("fph: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if !ac.HasDesiredHeading || math.Abs(ac.DesiredHeading-45) > 1e-9 {
		t.Errorf("DesiredHeading = %v", ac.DesiredHeading)
	}
	if ac.TurnDir != TurnShortest || ac.ImmediateHeading {
		t.Errorf("turn=%d imm=%v", ac.TurnDir, ac.ImmediateHeading)
	}
	if ac.Instruction != "Fly present heading" {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	// Alias fch.
	if r := e.Command(cs, "fhn 180"); !r.OK {
		t.Fatal(r.Message)
	}
	if r := e.Command(cs, "fch"); !r.OK {
		t.Fatalf("fch: %s", r.Message)
	}
	if math.Abs(e.mustGet(t, cs).DesiredHeading-180) > 1e-9 {
		t.Errorf("fch DesiredHeading = %v", e.mustGet(t, cs).DesiredHeading)
	}
}

func TestCommand_ClimbMaintain(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Approach altitude at 10 NM is well above field; force known alt via Get/re-command.
	// Place via bearing so Alt is exact.
	e.Delete(cs)
	r := e.CommandLine("add i l j -270 15 5000")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs = r.Added[0].Callsign
	if math.Abs(r.Added[0].Alt-5000) > 1e-6 {
		t.Fatalf("setup alt = %v", r.Added[0].Alt)
	}

	// Climb.
	r = e.Command(cs, "cm 14000")
	if !r.OK {
		t.Fatalf("cm climb: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if !ac.HasDesiredAlt || math.Abs(ac.DesiredAlt-14000) > 1e-9 {
		t.Errorf("DesiredAlt = %v", ac.DesiredAlt)
	}
	if !strings.Contains(strings.ToLower(ac.Instruction), "climb") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}
	// Domain does not change current Alt.
	if math.Abs(ac.Alt-5000) > 1e-6 {
		t.Errorf("Alt mutated: %v", ac.Alt)
	}

	// Descend.
	r = e.Command(cs, "cm 3000")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac = e.mustGet(t, cs)
	if !strings.Contains(strings.ToLower(ac.Instruction), "descend") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	// Maintain (same alt).
	r = e.Command(cs, "cm 5000")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac = e.mustGet(t, cs)
	if !strings.Contains(strings.ToLower(ac.Instruction), "maintain") ||
		strings.Contains(strings.ToLower(ac.Instruction), "climb") ||
		strings.Contains(strings.ToLower(ac.Instruction), "descend") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	// Alias dm.
	r = e.Command(cs, "dm 9000")
	if !r.OK {
		t.Fatalf("dm: %s", r.Message)
	}
	if math.Abs(e.mustGet(t, cs).DesiredAlt-9000) > 1e-9 {
		t.Errorf("dm DesiredAlt = %v", e.mustGet(t, cs).DesiredAlt)
	}

	for _, line := range []string{"cm", "cm NaN"} {
		if e.Command(cs, line).OK {
			t.Errorf("%q should fail", line)
		}
	}
}

func TestCommand_Speed(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	orig := e.mustGet(t, cs).Speed

	aliases := []string{"spd 250", "speed 210", "slow 180", "sln 160", "sl 150", "ds 140", "is 130"}
	for _, line := range aliases {
		r := e.Command(cs, line)
		if !r.OK {
			t.Fatalf("%q: %s", line, r.Message)
		}
	}
	ac := e.mustGet(t, cs)
	if !ac.HasDesiredSpeed || math.Abs(ac.DesiredSpeed-130) > 1e-9 {
		t.Errorf("DesiredSpeed = %v has=%v", ac.DesiredSpeed, ac.HasDesiredSpeed)
	}
	// Current speed unchanged until tick.
	if math.Abs(ac.Speed-orig) > 1e-9 {
		t.Errorf("Speed mutated: %v → %v", orig, ac.Speed)
	}
	if !strings.Contains(ac.Instruction, "130") {
		t.Errorf("Instruction = %q", ac.Instruction)
	}

	// Zero allowed; negative rejected.
	if r := e.Command(cs, "spd 0"); !r.OK {
		t.Fatalf("spd 0: %s", r.Message)
	}
	if e.mustGet(t, cs).DesiredSpeed != 0 {
		t.Error("DesiredSpeed 0")
	}
	if e.Command(cs, "spd -10").OK {
		t.Error("negative speed should fail")
	}
	if e.Command(cs, "spd").OK {
		t.Error("missing arg should fail")
	}
}

func TestCommand_FlightPlan_IFR(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	// FL-style cruise (220 → 22000).
	r := e.Command(cs, "fp b738 220 kbos lucos sey parch ccc rober kjfk")
	if !r.OK {
		t.Fatalf("fp: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Rules != RulesIFR {
		t.Errorf("Rules = %s", ac.Rules)
	}
	if ac.Type != "B738" {
		t.Errorf("Type = %s", ac.Type)
	}
	if ac.CruiseAlt != 22000 {
		t.Errorf("CruiseAlt = %d, want 22000", ac.CruiseAlt)
	}
	if ac.Route != "kbos lucos sey parch ccc rober kjfk" {
		t.Errorf("Route = %q", ac.Route)
	}
	if ac.Dep != "KBOS" {
		t.Errorf("Dep = %s", ac.Dep)
	}
	if ac.Arr != "KJFK" {
		t.Errorf("Arr = %s", ac.Arr)
	}

	// Full-feet cruise.
	r = e.Command(cs, "fp B190 16000 KBOS BOSOX ALB KALB")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.CruiseAlt != 16000 {
		t.Errorf("CruiseAlt feet = %d", ac.CruiseAlt)
	}
	if ac.Type != "B190" {
		t.Errorf("Type = %s", ac.Type)
	}

	// Type + alt only (no route).
	r = e.Command(cs, "fp c172 90")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.CruiseAlt != 9000 {
		t.Errorf("FL90 → %d", ac.CruiseAlt)
	}
	if ac.Route != "" {
		t.Errorf("Route should be empty, got %q", ac.Route)
	}

	// Validation.
	for _, line := range []string{"fp", "fp b738", "fp b738 NaN", "fp b738 -1"} {
		if e.Command(cs, line).OK {
			t.Errorf("%q should fail", line)
		}
	}
}

func TestCommand_FlightPlan_VFR(t *testing.T) {
	e := loadKBTVEngine(t)
	// Start IFR then switch to VFR plan.
	cs := addAirborne(t, e)
	r := e.Command(cs, "vp c172 8500 kbos dct gps kalb")
	if !r.OK {
		t.Fatalf("vp: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Rules != RulesVFR {
		t.Errorf("Rules = %s", ac.Rules)
	}
	if ac.Type != "C172" {
		t.Errorf("Type = %s", ac.Type)
	}
	if ac.CruiseAlt != 8500 {
		t.Errorf("CruiseAlt = %d", ac.CruiseAlt)
	}
	if ac.Dep != "KBOS" || ac.Arr != "KALB" {
		t.Errorf("Dep/Arr = %s/%s", ac.Dep, ac.Arr)
	}
	if ac.Route != "kbos dct gps kalb" {
		t.Errorf("Route = %q", ac.Route)
	}
}

func TestCommand_Remarks(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	r := e.Command(cs, "remarks Request VFR closed traffic on 22R")
	if !r.OK {
		t.Fatalf("remarks: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Remarks != "Request VFR closed traffic on 22R" {
		t.Errorf("Remarks = %q", ac.Remarks)
	}

	// Embedded callsign form preserves text.
	r = e.CommandLine(cs + ", remarks /v/ charts")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).Remarks != "/v/ charts" {
		t.Errorf("Remarks = %q", e.mustGet(t, cs).Remarks)
	}

	if e.Command(cs, "remarks").OK {
		t.Error("empty remarks should fail")
	}
}

func TestCommand_VectorCallsignPrefixAndSelection(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	// Selected session callsign.
	r := e.Command(cs, "cm 4000")
	if !r.OK {
		t.Fatal(r.Message)
	}
	// Prefix form with comma.
	r = e.CommandLine(cs + ", fh 090")
	if !r.OK {
		t.Fatal(r.Message)
	}
	// Prefix form without comma.
	r = e.CommandLine(cs + " spd 200")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if math.Abs(ac.DesiredHeading-90) > 1e-9 || math.Abs(ac.DesiredSpeed-200) > 1e-9 {
		t.Errorf("hdg=%v spd=%v", ac.DesiredHeading, ac.DesiredSpeed)
	}

	// Unknown aircraft.
	r = e.Command("ZZZZZ", "fh 10")
	if r.OK || !strings.Contains(r.Message, "not found") {
		t.Errorf("unknown: %+v", r)
	}
}

func TestParseCruiseAltitude(t *testing.T) {
	cases := []struct {
		in   float64
		want int
	}{
		{0, 0},
		{90, 9000},
		{220, 22000},
		{999, 99900},
		{1000, 1000},
		{8500, 8500},
		{29000, 29000},
	}
	for _, tc := range cases {
		if got := parseCruiseAltitude(tc.in); got != tc.want {
			t.Errorf("parseCruiseAltitude(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestLooksLikeAirport(t *testing.T) {
	yes := []string{"KBOS", "bos", "JFK", "kbtv"}
	no := []string{"", "AB", "ABCDE", "K12", "12A", "BOSOX"}
	for _, s := range yes {
		if !looksLikeAirport(s) {
			t.Errorf("looksLikeAirport(%q) = false", s)
		}
	}
	for _, s := range no {
		if looksLikeAirport(s) {
			t.Errorf("looksLikeAirport(%q) = true", s)
		}
	}
}

func TestSnapshot_IncludesVectorTargets(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	_ = e.Command(cs, "fh 100")
	_ = e.Command(cs, "cm 12000")
	_ = e.Command(cs, "spd 240")
	snap := e.Snapshot()
	if len(snap.Aircraft) != 1 {
		t.Fatalf("n = %d", len(snap.Aircraft))
	}
	ac := snap.Aircraft[0]
	if !ac.HasDesiredHeading || ac.DesiredHeading != 100 {
		t.Errorf("hdg snap: %+v", ac)
	}
	if !ac.HasDesiredAlt || ac.DesiredAlt != 12000 {
		t.Errorf("alt snap: %+v", ac)
	}
	if !ac.HasDesiredSpeed || ac.DesiredSpeed != 240 {
		t.Errorf("spd snap: %+v", ac)
	}
}

func TestCommand_VectorsMoveOnTick(t *testing.T) {
	// PR 4: Tick consumes Desired* targets (kinematics).
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	_ = e.CommandLine("un")
	before := e.mustGet(t, cs)
	_ = e.Command(cs, "fh 010")
	_ = e.Command(cs, "cm 20000")
	_ = e.Command(cs, "spd 300")
	e.Tick(time.Second)
	after := e.mustGet(t, cs)
	// Position / heading / alt / speed should respond to vectors.
	moved := math.Abs(after.Lat-before.Lat) > 1e-9 || math.Abs(after.Lon-before.Lon) > 1e-9
	turned := math.Abs(headingDelta(after.Heading, before.Heading)) > 0.1
	climbed := after.Alt > before.Alt+1
	sped := after.Speed > before.Speed+1
	if !moved && !turned {
		t.Error("tick should turn/move under fh")
	}
	if !climbed {
		t.Error("tick should climb under cm")
	}
	if !sped {
		t.Error("tick should accelerate under spd")
	}
	// Desired targets retained.
	if !after.HasDesiredHeading || after.DesiredHeading != 10 {
		t.Errorf("desired hdg lost: %v", after.DesiredHeading)
	}
}
