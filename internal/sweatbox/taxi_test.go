package sweatbox

import (
	"math"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/geo"
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

	// Via C K J to 33
	plan, errMsg = g.PlanTaxi("C", []string{"C", "K", "J", "33"}, nil)
	if errMsg != "" {
		t.Fatalf("C K J 33: %s", errMsg)
	}
	if len(plan.Waypoints) < 3 {
		t.Errorf("expected multi-point path, got %d", len(plan.Waypoints))
	}

	// Parking destination @GA9 (snaps near J).
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
	// Last waypoint should be parking (even if within snap of last taxiway point).
	last := plan.Waypoints[len(plan.Waypoints)-1]
	ps := g.Surface("GA9")
	if ps == nil || last != ps.Points[0] {
		t.Errorf("last wp = %v, parking = %v", last, ps.Points[0])
	}
	if len(plan.Waypoints) < 2 {
		t.Fatal("parking within snap of taxiway must still append parking as final point")
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

// Issue 1 regression: hold short of 19 on A→B→19 must be B∩19, not A∩19.
func TestPlanTaxi_HoldAtCorrectCrossing_KBTV(t *testing.T) {
	g := NewGraph(loadKBTV(t))

	bX19, _, _, ok := g.FindIntersection("B", "19")
	if !ok {
		t.Fatal("B and 19 must intersect on KBTV")
	}
	aX19, _, _, ok := g.FindIntersection("A", "19")
	if !ok {
		t.Fatal("A and 19 must intersect on KBTV")
	}
	// Sanity: the two crossings are far apart (~560 m).
	if geo.Distance(bX19.Lat, bX19.Lon, aX19.Lat, aX19.Lon) < 200 {
		t.Fatalf("test setup: A∩19 and B∩19 too close")
	}

	plan, errMsg := g.PlanTaxi("A", []string{"B", "19"}, []string{"19"})
	if errMsg != "" {
		t.Fatalf("A→B→19 hs 19: %s", errMsg)
	}
	if len(plan.HoldAt) != 1 {
		t.Fatalf("HoldAt len = %d", len(plan.HoldAt))
	}
	h := plan.HoldAt[0]
	if h.Name != "19" {
		t.Errorf("HoldAt name = %q", h.Name)
	}
	if h.WaypointIndex < 0 || h.WaypointIndex >= len(plan.Waypoints) {
		t.Fatalf("WaypointIndex = %d, path len %d", h.WaypointIndex, len(plan.Waypoints))
	}
	if plan.Waypoints[h.WaypointIndex] != h.Point {
		t.Errorf("Waypoints[i]=%v HoldAt.Point=%v", plan.Waypoints[h.WaypointIndex], h.Point)
	}
	// Hold must be near B∩19, not A∩19.
	dB := geo.Distance(h.Point.Lat, h.Point.Lon, bX19.Lat, bX19.Lon)
	dA := geo.Distance(h.Point.Lat, h.Point.Lon, aX19.Lat, aX19.Lon)
	if dB > DefaultIntersectionTolM {
		t.Errorf("hold is %.1fm from B∩19 (want ≤%.1f); A∩19 dist=%.1fm point=%v",
			dB, DefaultIntersectionTolM, dA, h.Point)
	}
	if dA < dB {
		t.Errorf("hold closer to A∩19 (%.1fm) than B∩19 (%.1fm)", dA, dB)
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
		// Issue 5: hold of 19 is not on C–K–J–33 path (even though C∩19 exists).
		{"off-route hold", "C", []string{"C", "K", "J", "33"}, []string{"19"}, "not on the taxi route"},
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
	// BHP must land on path.
	if plan.HoldAt[0].Name != "BHP" {
		t.Errorf("hold0 = %q", plan.HoldAt[0].Name)
	}
	if plan.HoldAt[0].WaypointIndex < 0 {
		t.Errorf("BHP WaypointIndex = %d", plan.HoldAt[0].WaypointIndex)
	}
	if math.Abs(plan.HoldAt[0].Point.Lat-10.001) > 1e-9 {
		t.Errorf("BHP point = %v", plan.HoldAt[0].Point)
	}
	// Runway 9 hold at B∩9 (entry onto runway), on path.
	if plan.HoldAt[1].Name != "9" {
		t.Errorf("hold1 = %q", plan.HoldAt[1].Name)
	}
	if plan.HoldAt[1].WaypointIndex < 0 || plan.HoldAt[1].WaypointIndex >= len(plan.Waypoints) {
		t.Fatalf("rwy WaypointIndex = %d", plan.HoldAt[1].WaypointIndex)
	}
	bX9, ok := intersectionOnSurface(g, "B", "9")
	if !ok {
		t.Fatal("B∩9 missing")
	}
	if geo.Distance(plan.HoldAt[1].Point.Lat, plan.HoldAt[1].Point.Lon, bX9.Lat, bX9.Lon) > DefaultIntersectionTolM {
		t.Errorf("rwy hold %v far from B∩9 %v", plan.HoldAt[1].Point, bX9)
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
	if plan.Waypoints[len(plan.Waypoints)-1] != g.Surface("G1").Points[0] {
		t.Error("final waypoint must be parking")
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
	aSurf := g.Surface("A")
	if aSurf == nil {
		t.Fatal("taxiway A missing")
	}

	// FindIntersection(nameA,nameB) → (pt on A, idxA, idxB, ok).
	// C∩A: index on A is idxB; A∩B: index on A is idxA.
	_, _, iEntry, ok := g.FindIntersection("C", "A")
	if !ok {
		t.Fatal("C∩A")
	}
	_, iExit, _, ok := g.FindIntersection("A", "B")
	if !ok {
		t.Fatal("A∩B")
	}
	lo, hi := iEntry, iExit
	if lo > hi {
		lo, hi = hi, lo
	}
	wantSpan := hi - lo + 1
	if wantSpan < 2 {
		t.Fatalf("unexpected A index span %d..%d", lo, hi)
	}

	// Current on C: taxi A B — walk A from C∩A to A∩B inclusive of intermediate A vertices.
	plan, errMsg := g.PlanTaxi("C", []string{"A", "B"}, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	for i := lo; i <= hi; i++ {
		found := false
		for _, wp := range plan.Waypoints {
			if wp == aSurf.Points[i] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing A[%d]=%v in path %v", i, aSurf.Points[i], plan.Waypoints)
		}
	}
	if len(plan.Waypoints) < wantSpan {
		t.Errorf("path len %d < A span %d", len(plan.Waypoints), wantSpan)
	}
}

// Issue 2: dense polyline vertices (~10 m spacing) must not collapse under 100 ft snap.
func TestPlanTaxi_DensePolylineRetainsVertices(t *testing.T) {
	// Build a N–S taxiway with 20 points ~10 m apart, then turn onto short east taxiway.
	const n = 20
	pts := make([]Point, n)
	// ~10 m in latitude ≈ 10/111320 degrees.
	dLat := 10.0 / 111320.0
	for i := 0; i < n; i++ {
		pts[i] = Point{Lat: 40.0 + float64(i)*dLat, Lon: -75.0}
	}
	// End of A shares last point with start of B.
	bPts := []Point{
		pts[n-1],
		{Lat: pts[n-1].Lat, Lon: -75.0 + dLat}, // ~10 m east at mid-lat (approx)
	}
	apt := Airport{
		Surfaces: []Surface{
			{Kind: SurfaceTaxiway, Name: "A", Points: pts},
			{Kind: SurfaceTaxiway, Name: "B", Points: bPts},
		},
	}
	g := NewGraph(&apt)

	// Verify consecutive spacing is well under 100 ft.
	d := geo.Distance(pts[0].Lat, pts[0].Lon, pts[1].Lat, pts[1].Lon)
	if d > 15 || d < 5 {
		t.Fatalf("setup spacing = %.1fm, want ~10m", d)
	}

	plan, errMsg := g.PlanTaxi("A", []string{"A", "B"}, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	// When already on A, path starts at A∩B (last point) only for the A leg
	// (from unset, to = last). That yields a single point on A then B.
	// Use current off A so we walk full A from start: place a parking as current
	// that intersects first point of A.
	apt2 := apt
	apt2.Surfaces = append(apt2.Surfaces, Surface{
		Kind: SurfaceParking, Name: "P1",
		Points: []Point{pts[0]},
	})
	g2 := NewGraph(&apt2)
	// Taxi from P1 (intersects A[0]) along A to B — first step A must ∩ current P1.
	plan, errMsg = g2.PlanTaxi("P1", []string{"A", "B"}, nil)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	// Full walk of A from index 0 to n-1 inclusive = n vertices, then B may add one more.
	// With 100 ft stitch dedup this would collapse to ~5; with 1 m stitch we keep all n.
	countOnA := 0
	for _, wp := range plan.Waypoints {
		for _, ap := range pts {
			if wp == ap {
				countOnA++
				break
			}
		}
	}
	if countOnA < n {
		t.Fatalf("retained %d/%d A vertices (100ft collapse bug if ~5); path=%v",
			countOnA, n, plan.Waypoints)
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
	fwd := walkPolyline(s, Point{0, 0}, Point{2, 0})
	if len(fwd) != 3 {
		t.Fatalf("fwd len = %d", len(fwd))
	}
	rev := walkPolyline(s, Point{3, 0}, Point{1, 0})
	if len(rev) != 3 || rev[0].Lat != 3 || rev[2].Lat != 1 {
		t.Fatalf("rev = %v", rev)
	}
	same := walkPolyline(s, Point{1, 0}, Point{1, 0})
	if len(same) != 1 {
		t.Fatalf("same = %v", same)
	}
	if walkPolyline(nil, Point{}, Point{}) != nil {
		t.Error("nil surface")
	}
}

func TestAppendUniquePoints_StitchEpsNotSnap(t *testing.T) {
	// Points ~10 m apart must all be kept with pathStitchEpsM.
	dLat := 10.0 / 111320.0
	src := []Point{
		{40.0, -75.0},
		{40.0 + dLat, -75.0},
		{40.0 + 2*dLat, -75.0},
	}
	dst := appendUniquePoints(nil, src, pathStitchEpsM)
	if len(dst) != 3 {
		t.Fatalf("stitch eps kept %d, want 3 (would be 1 under 100ft snap)", len(dst))
	}
	// Exact duplicate still collapses.
	dst = appendUniquePoints(nil, []Point{{1, 2}, {1, 2}, {3, 4}}, pathStitchEpsM)
	if len(dst) != 2 {
		t.Fatalf("exact dup: %v", dst)
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
	if plan.HoldAt[0].WaypointIndex < 0 {
		t.Error("hold index")
	}
}

func TestPlanTaxi_MaxHoldsBoundary(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	// 10 holds of B is ok (B is on A→B→19); 11 fails count before path.
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

func TestPlanTaxi_HOLDOffRouteRejected(t *testing.T) {
	apt := Airport{
		Surfaces: []Surface{
			{Kind: SurfaceTaxiway, Name: "A", Points: []Point{{10, 20}, {10.001, 20}}},
			{Kind: SurfaceTaxiway, Name: "B", Points: []Point{{10.001, 20}, {10.002, 20}}},
			// HOLD far from path.
			{Kind: SurfaceHold, Name: "FAR", Points: []Point{{11, 21}}},
		},
	}
	g := NewGraph(&apt)
	_, errMsg := g.PlanTaxi("A", []string{"A", "B"}, []string{"FAR"})
	if !strings.Contains(errMsg, "not on the taxi route") {
		t.Errorf("got %q", errMsg)
	}
}

func TestPlanTaxi_HoldOnCurrentDestinationSurface(t *testing.T) {
	// Already on runway 19, taxi via B (off then... 19→B): hold short of 19 uses
	// firstPathPointOnSurface when hold is the first step and current.
	g := NewGraph(loadKBTV(t))
	// Route must leave 19 onto a connected surface then... actually hold of 19
	// while steps start with 19: PlanTaxi("19", []string{"19", "B"}, []string{"19"})
	// sameSurface first step and current — not Already there (multi-step).
	plan, errMsg := g.PlanTaxi("19", []string{"19", "B"}, []string{"19"})
	if errMsg != "" {
		t.Fatalf("%s", errMsg)
	}
	if len(plan.HoldAt) != 1 || plan.HoldAt[0].WaypointIndex < 0 {
		t.Fatalf("HoldAt=%+v", plan.HoldAt)
	}
}

func TestInsertWaypoint(t *testing.T) {
	wps, idx := insertWaypoint(nil, Point{1, 2}, pathStitchEpsM)
	if len(wps) != 1 || idx != 0 {
		t.Fatalf("empty: %v idx=%d", wps, idx)
	}
	// Already present within eps
	wps2, idx2 := insertWaypoint(wps, Point{1, 2}, pathStitchEpsM)
	if len(wps2) != 1 || idx2 != 0 {
		t.Fatalf("dup: %v idx=%d", wps2, idx2)
	}
	// Insert before closer of two distant points (~5 m from first).
	dLat := 5.0 / 111320.0
	base := []Point{{40.0, -75.0}, {40.1, -75.0}}
	wps3, idx3 := insertWaypoint(base, Point{40.0 + dLat, -75.0}, pathStitchEpsM)
	if idx3 != 0 || len(wps3) != 3 {
		t.Fatalf("insert: %v idx=%d", wps3, idx3)
	}
}

func TestHoldNearRouteSurface_CurrentOnly(t *testing.T) {
	apt := Airport{Surfaces: []Surface{
		{Kind: SurfaceTaxiway, Name: "A", Points: []Point{{10, 20}, {10.001, 20}}},
		{Kind: SurfaceHold, Name: "H1", Points: []Point{{10, 20}}},
	}}
	g := NewGraph(&apt)
	if !g.holdNearRouteSurface(Point{10, 20}, nil, "A") {
		t.Error("should be near current A")
	}
	if g.holdNearRouteSurface(Point{0, 0}, []string{"A"}, "") {
		t.Error("far point")
	}
}

func TestIntersectionOnSurface_PrefersB(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	p, ok := intersectionOnSurface(g, "A", "B")
	if !ok {
		t.Fatal("A∩B")
	}
	// Point should be on B (exact shared vertex on both).
	b := g.Surface("B")
	found := false
	for _, q := range b.Points {
		if q == p {
			found = true
			break
		}
	}
	if !found {
		// Within snap of a B point is also fine.
		if g.PointIndexWithin("B", p) < 0 {
			t.Errorf("point %v not on B", p)
		}
	}
}
