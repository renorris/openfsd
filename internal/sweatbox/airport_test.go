package sweatbox

import (
	"testing"

	"github.com/renorris/openfsd/internal/geo"
)

func loadKBTV(t *testing.T) *Airport {
	t.Helper()
	data := kbtvFixture(t, "KBTV_example.apt")
	apt, errs := ParseAPT(string(data))
	if len(errs) != 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return &apt
}

func TestNewGraph_KBTVIntersections(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	if g.TolM() != DefaultIntersectionTolM {
		t.Errorf("TolM = %v, want %v", g.TolM(), DefaultIntersectionTolM)
	}
	if g.Airport() == nil || g.Airport().ICAO != "KBTV" {
		t.Fatalf("Airport ICAO = %v", g.Airport())
	}

	// Known shared waypoints from fixture.
	pairs := [][2]string{
		{"A", "B"},
		{"A", "C"},
		{"A", "19"},
		{"B", "1"},
		{"C", "19"},
		{"19/1", "33/15"},
		{"D", "E"},
		{"F", "G"},
		{"J", "K"},
		{"GA9", "J"}, // parking near taxiway J
	}
	for _, p := range pairs {
		if !g.Intersect(p[0], p[1]) {
			t.Errorf("expected intersect %s ↔ %s", p[0], p[1])
		}
		pt, ia, ib, ok := g.FindIntersection(p[0], p[1])
		if !ok {
			t.Errorf("FindIntersection(%s,%s) ok=false", p[0], p[1])
			continue
		}
		if ia < 0 || ib < 0 {
			t.Errorf("FindIntersection(%s,%s) bad indices %d,%d", p[0], p[1], ia, ib)
		}
		if pt.Lat == 0 && pt.Lon == 0 {
			t.Errorf("FindIntersection(%s,%s) zero point", p[0], p[1])
		}
	}

	// Non-intersecting.
	if g.Intersect("L", "M") {
		t.Error("L and M should not intersect")
	}
	if g.Intersect("A", "D") {
		t.Error("A and D should not intersect")
	}

	// Self-intersect.
	if !g.Intersect("A", "A") {
		t.Error("surface should intersect itself")
	}

	// Missing surface.
	if g.Intersect("A", "ZZZ") {
		t.Error("missing surface should not intersect")
	}
	if g.Surface("NOPE") != nil {
		t.Error("Surface(NOPE) want nil")
	}
}

func TestGraph_Neighbors(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	n := g.Neighbors("A")
	if len(n) == 0 {
		t.Fatal("A should have neighbors")
	}
	// A touches 19/1, 33/15, B, C, E, G (order by airport file index).
	wantAny := map[string]bool{"19/1": true, "B": true, "C": true}
	for _, w := range []string{"19/1", "B", "C"} {
		found := false
		for _, x := range n {
			if x == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Neighbors(A) missing %s: %v", w, n)
		}
		_ = wantAny
	}
	if g.Neighbors("ZZZ") != nil {
		t.Error("Neighbors missing want nil")
	}
}

func TestGraph_ClosestWaypoint(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	// Point near first waypoint of taxiway A.
	pt, idx, dist, ok := g.ClosestWaypoint("A", 44.46512, -73.15227)
	if !ok {
		t.Fatal("ClosestWaypoint A failed")
	}
	if idx != 0 {
		t.Errorf("idx = %d, want 0", idx)
	}
	if dist > 1 {
		t.Errorf("dist = %v, want ~0", dist)
	}
	if pt.Lat != 44.46512 {
		t.Errorf("pt.Lat = %v", pt.Lat)
	}
	_, _, _, ok = g.ClosestWaypoint("NOPE", 0, 0)
	if ok {
		t.Error("missing surface should fail")
	}
}

func TestGraph_PointIndexWithin(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	s := g.Surface("A")
	if s == nil || len(s.Points) < 2 {
		t.Fatal("taxiway A missing")
	}
	i := g.PointIndexWithin("A", s.Points[2])
	if i != 2 {
		t.Errorf("PointIndexWithin = %d, want 2", i)
	}
	// Far point.
	if g.PointIndexWithin("A", Point{Lat: 0, Lon: 0}) != -1 {
		t.Error("far point should be -1")
	}
}

func TestNewGraphTol_CustomAndNil(t *testing.T) {
	g := NewGraphTol(nil, -1)
	if g.TolM() != DefaultIntersectionTolM {
		t.Errorf("nil apt tol = %v", g.TolM())
	}
	if g.Airport() == nil {
		t.Error("Airport() nil")
	}
	// Tiny tol: exact same coords only.
	apt := &Airport{Surfaces: []Surface{
		{Kind: SurfaceTaxiway, Name: "X", Points: []Point{{1, 2}, {1.001, 2.001}}},
		{Kind: SurfaceTaxiway, Name: "Y", Points: []Point{{1, 2}, {3, 4}}},
	}}
	g2 := NewGraphTol(apt, 1.0) // 1 meter
	if !g2.Intersect("X", "Y") {
		// 1,2 is exact match — DistanceSq is 0.
		t.Error("exact shared point should intersect at 1m tol")
	}
	// Move Y's first point slightly more than 1m at equator-ish (lat 1).
	// ~0.00002 deg lat ≈ 2.2m
	apt3 := &Airport{Surfaces: []Surface{
		{Kind: SurfaceTaxiway, Name: "X", Points: []Point{{1, 2}}},
		{Kind: SurfaceTaxiway, Name: "Y", Points: []Point{{1.00002, 2}}},
	}}
	g3 := NewGraphTol(apt3, 1.0)
	d := geo.Distance(1, 2, 1.00002, 2)
	if d <= 1 && !g3.Intersect("X", "Y") {
		t.Errorf("d=%v should intersect", d)
	}
	if d > 1 && g3.Intersect("X", "Y") {
		t.Errorf("d=%v should not intersect at 1m", d)
	}
}

func TestGraph_NilReceiver(t *testing.T) {
	var g *Graph
	if g.Airport() != nil {
		t.Error("nil Graph.Airport")
	}
	if g.TolM() != DefaultIntersectionTolM {
		t.Error("nil Graph.TolM")
	}
	if g.Surface("A") != nil {
		t.Error("nil Graph.Surface")
	}
	if g.Intersect("A", "B") {
		t.Error("nil Graph.Intersect")
	}
	if g.Neighbors("A") != nil {
		t.Error("nil Graph.Neighbors")
	}
	_, _, _, ok := g.ClosestWaypoint("A", 0, 0)
	if ok {
		t.Error("nil ClosestWaypoint")
	}
	if g.PointIndexWithin("A", Point{}) != -1 {
		t.Error("nil PointIndexWithin")
	}
}

func TestGraph_RunwayLookup(t *testing.T) {
	g := NewGraph(loadKBTV(t))
	if g.Surface("19") == nil || g.Surface("19").Kind != SurfaceRunway {
		t.Error("lookup by RwyA")
	}
	if g.Surface("1") == nil {
		t.Error("lookup by RwyB")
	}
	if g.Surface("19/1") == nil {
		t.Error("lookup by combined")
	}
	// Neighbors via designator
	n := g.Neighbors("19")
	if len(n) == 0 {
		t.Error("Neighbors via designator")
	}
}

func TestDefaultConstants(t *testing.T) {
	if MaxTaxiSteps != 100 {
		t.Errorf("MaxTaxiSteps = %d", MaxTaxiSteps)
	}
	if MaxHoldPoints != 10 {
		t.Errorf("MaxHoldPoints = %d", MaxHoldPoints)
	}
	// 100 ft in meters
	if DefaultIntersectionTolM < 30.0 || DefaultIntersectionTolM > 31.0 {
		t.Errorf("DefaultIntersectionTolM = %v", DefaultIntersectionTolM)
	}
}
