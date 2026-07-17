package sweatbox

import (
	"strings"
	"testing"
)

func TestPlanTaxi_KBTVHappyPaths(t *testing.T) {
	g := NewGraph(loadKBTV(t))

	// A → B → 33 (via designator)
	plan, errMsg := g.PlanTaxi("A", []string{"A", "B", "33"}, nil)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps = %v", plan.Steps)
	}
	if plan.Steps[2] != "33/15" {
		t.Errorf("runway canonical name = %q", plan.Steps[2])
	}
	if plan.Parking != "" {
		t.Errorf("parking = %q", plan.Parking)
	}
	if len(plan.Waypoints) < 2 {
		t.Fatalf("waypoints too short: %v", plan.Waypoints)
	}

	// Current on A, taxi B 19 — first step B intersects A.
	plan, errMsg = g.PlanTaxi("A", []string{"B", "19"}, []string{"19"})
	if errMsg != "" {
		t.Fatalf("A→B→19: %s", errMsg)
	}
	if len(plan.Holds) != 1 || plan.Holds[0] != "19" {
		t.Errorf("holds = %v", plan.Holds)
	}
	if len(plan.HoldAt) != 1 {
		t.Fatalf("HoldAt len = %d", len(plan.HoldAt))
	}
	if plan.HoldAt[0].Name != "19" {
		t.Errorf("HoldAt name = %q", plan.HoldAt[0].Name)
	}
	if plan.HoldAt[0].Point.Lat == 0 {
		t.Error("HoldAt point unset")
	}

	// Via C K J to 33
	plan, errMsg = g.PlanTaxi("C", []string{"C", "K", "J", "33"}, nil)
	if errMsg != "" {
		t.Fatalf("C K J 33: %s", errMsg)
	}
	if len(plan.Waypoints) < 3 {
		t.Errorf("expected multi-point path, got %d", len(plan.Waypoints))
	}

	// Parking destination @G1 via A (A is near gates; G1 may be far — still allowed)
	// Use GA9 which snaps to J.
	plan, errMsg = g.PlanTaxi("J", []string{"J", "@GA9"}, nil)
	if errMsg != "" {
		t.Fatalf("J @GA9: %s", errMsg)
	}
	if plan.Parking != "GA9" {
		t.Errorf("Parking = %q", plan.Parking)
	}
	if len(plan.Steps) != 1 || plan.Steps[0] != "J" {
		t.Errorf("Steps = %v", plan.Steps)
	}
	// Last waypoint should be parking.
	last := plan.Waypoints[len(plan.Waypoints)-1]
	ps := g.Surface("GA9")
	if ps == nil || last.Lat != ps.Points[0].Lat {
		t.Errorf("last wp = %v, parking = %v", last, ps)
	}

	// Bare parking name as last step.
	plan, errMsg = g.PlanTaxi("J", []string{"J", "GA9"}, nil)
	if errMsg != "" {
		t.Fatalf("bare parking: %s", errMsg)
	}
	if plan.Parking != "GA9" {
		t.Errorf("Parking = %q", plan.Parking)
	}
}

func TestPlanTaxi_ValidationErrors(t *testing.T) {
	g := NewGraph(loadKBTV(t))

	tests := []struct {
		name    string
		current string
		steps   []string
		holds   []string
		wantSub string
	}{
		{"empty steps", "A", nil, nil, "Must specify at least one"},
		{"too many steps", "A", manySteps(101), nil, "Too many taxi steps"},
		{"too many holds", "A", []string{"B"}, manyHolds(11), "Too many hold points"},
		{"unknown step", "A", []string{"ZZZ"}, nil, "Unknown step in taxi route: ZZZ"},
		{"parking not last", "A", []string{"@G1", "B"}, nil, "Only the last step can be a parking space"},
		{"unknown parking", "A", []string{"A", "@NOPE"}, nil, "Unknown parking space"},
		{"bad at token", "A", []string{"A", "@"}, nil, "Unknown step"},
		{"hold parking", "A", []string{"B"}, []string{"G1"}, "Can't hold short of a parking space"},
		{"hold missing", "A", []string{"B"}, []string{"ZZZ"}, "Runway/taxiway not found"},
		{"already there", "A", []string{"A"}, nil, "Already there!"},
		{"already there rwy", "19", []string{"1"}, nil, "Already there!"},
		{"first no intersect", "L", []string{"M"}, nil, "First step of taxi route must intersect"},
		{"pair no intersect", "A", []string{"A", "D"}, nil, "do not intersect"},
		{"duplicate consecutive", "A", []string{"A", "A", "B"}, nil, "occurs twice in a row"},
		{"off surface empty current ok if steps ok", "", []string{"A", "B"}, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, errMsg := g.PlanTaxi(tc.current, tc.steps, tc.holds)
			if tc.wantSub == "" {
				if errMsg != "" {
					t.Fatalf("want success, got %q", errMsg)
				}
				return
			}
			if !strings.Contains(errMsg, tc.wantSub) {
				t.Fatalf("err = %q, want substring %q", errMsg, tc.wantSub)
			}
		})
	}
}

func manySteps(n int) []string {
	// Alternate A/B would fail consecutive check; use unique invalid for count test —
	// count is checked before surface lookup... actually count is first, then normalize.
	// For max steps we only need len > 100; unknown steps come after.
	// Build 101 "A" would fail consecutive after count check order:
	// count is checked first — good.
	out := make([]string, n)
	for i := range out {
		out[i] = "A"
	}
	return out
}

func manyHolds(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "A"
	}
	return out
}

func TestPlanTaxi_HoldShortNamedAndRunway(t *testing.T) {
	// Synthetic airport with a HOLD and taxiways.
	apt := Airport{
		ICAO: "XXXX",
		Surfaces: []Surface{
			{Kind: SurfaceTaxiway, Name: "A", Points: []Point{
				{Lat: 10.0, Lon: 20.0},
				{Lat: 10.001, Lon: 20.0},
				{Lat: 10.002, Lon: 20.0},
			}},
			{Kind: SurfaceTaxiway, Name: "B", Points: []Point{
				{Lat: 10.002, Lon: 20.0},
				{Lat: 10.002, Lon: 20.001},
			}},
			{Kind: SurfaceRunway, Name: "9/27", RwyA: "9", RwyB: "27", Points: []Point{
				{Lat: 10.002, Lon: 20.001},
				{Lat: 10.003, Lon: 20.002},
			}},
			{Kind: SurfaceHold, Name: "BHP", Points: []Point{
				{Lat: 10.001, Lon: 20.0},
			}},
			{Kind: SurfaceParking, Name: "G1", Points: []Point{
				{Lat: 10.0, Lon: 20.0001},
			}},
		},
	}
	g := NewGraph(&apt)

	plan, errMsg := g.PlanTaxi("A", []string{"A", "B", "9"}, []string{"BHP", "9"})
	if errMsg != "" {
		t.Fatalf("plan: %s", errMsg)
	}
	if len(plan.HoldAt) != 2 {
		t.Fatalf("HoldAt = %+v", plan.HoldAt)
	}
	// BHP uses its single point.
	if plan.HoldAt[0].Name != "BHP" {
		t.Errorf("hold0 = %q", plan.HoldAt[0].Name)
	}
	if plan.HoldAt[0].Point.Lat != 10.001 {
		t.Errorf("BHP point = %v", plan.HoldAt[0].Point)
	}
	if plan.HoldAt[1].Name != "9" {
		t.Errorf("hold1 = %q", plan.HoldAt[1].Name)
	}

	// HOLD cannot be a taxi step.
	_, errMsg = g.PlanTaxi("A", []string{"BHP"}, nil)
	if !strings.Contains(errMsg, "Unknown step") {
		t.Errorf("HOLD as step: %q", errMsg)
	}

	// Parking-only from current on A.
	plan, errMsg = g.PlanTaxi("A", []string{"@G1"}, nil)
	if errMsg != "" {
		t.Fatalf("@G1 only: %s", errMsg)
	}
	if plan.Parking != "G1" || len(plan.Steps) != 0 {
		t.Errorf("plan = %+v", plan)
	}
	if len(plan.Waypoints) == 0 {
		t.Error("expected waypoints to parking")
	}
}

func TestPlanTaxi_NilGraph(t *testing.T) {
	var g *Graph
	_, errMsg := g.PlanTaxi("A", []string{"B"}, nil)
	if errMsg == "" {
		t.Error("nil graph should error")
	}
}

func TestPlanTaxi_EmptyHoldToken(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	_, errMsg := g.PlanTaxi("A", []string{"B"}, []string{"  "})
	if !strings.Contains(errMsg, "not found") {
		t.Errorf("got %q", errMsg)
	}
}

func TestPlanTaxi_PathWalksIntermediate(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	// A has many points; A→E should walk along A between shared points.
	plan, errMsg := g.PlanTaxi("A", []string{"A", "E"}, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	// Intersection A∩E near north end; from start of A (if current=A, from unset)
	// path should at least include the intersection point.
	if len(plan.Waypoints) < 1 {
		t.Fatal("no waypoints")
	}
	// Current off A: from C onto A then to B
	plan, errMsg = g.PlanTaxi("C", []string{"A", "B"}, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	// Should include C∩A and A∩B and intermediates on A between them.
	if len(plan.Waypoints) < 2 {
		t.Fatalf("want path along A, got %d pts: %v", len(plan.Waypoints), plan.Waypoints)
	}
}

func TestPlanTaxi_AlreadyAtParking(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	_, errMsg := g.PlanTaxi("G1", []string{"@G1"}, nil)
	if errMsg != "Already there!" {
		t.Errorf("got %q", errMsg)
	}
}

func TestWalkPolyline_Directions(t *testing.T) {
	s := &Surface{Points: []Point{
		{0, 0}, {1, 0}, {2, 0}, {3, 0},
	}}
	fwd := walkPolyline(s, Point{0, 0}, Point{2, 0}, 30)
	if len(fwd) != 3 {
		t.Fatalf("fwd len = %d", len(fwd))
	}
	rev := walkPolyline(s, Point{3, 0}, Point{1, 0}, 30)
	if len(rev) != 3 || rev[0].Lat != 3 || rev[2].Lat != 1 {
		t.Fatalf("rev = %v", rev)
	}
	same := walkPolyline(s, Point{1, 0}, Point{1, 0}, 30)
	if len(same) != 1 {
		t.Fatalf("same = %v", same)
	}
	if walkPolyline(nil, Point{}, Point{}, 30) != nil {
		t.Error("nil surface")
	}
}

func TestAppendUniquePoints(t *testing.T) {
	var dst []Point
	dst = appendUniquePoints(dst, []Point{{1, 2}, {1, 2}, {3, 4}}, 30.48)
	if len(dst) != 2 {
		t.Fatalf("dst = %v", dst)
	}
}

func TestPlanTaxi_CaseInsensitive(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	plan, errMsg := g.PlanTaxi("a", []string{"b", "19"}, []string{"19"})
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if plan.Steps[0] != "B" {
		t.Errorf("steps[0]=%q", plan.Steps[0])
	}
}

func TestPlanTaxi_MaxHoldsBoundary(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	// 10 holds of A is ok (count only); 11 fails.
	holds := make([]string, 10)
	for i := range holds {
		holds[i] = "B"
	}
	_, errMsg := g.PlanTaxi("A", []string{"B", "19"}, holds)
	if errMsg != "" {
		t.Fatalf("10 holds: %s", errMsg)
	}
	holds = append(holds, "B")
	_, errMsg = g.PlanTaxi("A", []string{"B", "19"}, holds)
	if !strings.Contains(errMsg, "Too many hold points") {
		t.Errorf("11 holds: %q", errMsg)
	}
}
