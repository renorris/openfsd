package geo

import (
	"math"
	"math/rand"
	"testing"
)

func approxEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestDistance_SamePoint(t *testing.T) {
	d := Distance(37.6, -122.4, 37.6, -122.4)
	if d != 0 {
		t.Fatalf("same point distance = %v, want 0", d)
	}
}

func TestDistance_KnownPairs(t *testing.T) {
	// ~1 degree of latitude ≈ 111.19 km at mean Earth radius.
	const metersPerDegreeLat = (math.Pi * EarthRadius) / 180

	tests := []struct {
		name       string
		lat1, lon1 float64
		lat2, lon2 float64
		wantM      float64
		epsilon    float64
	}{
		{
			name: "1 degree latitude along meridian",
			lat1: 0, lon1: 0,
			lat2: 1, lon2: 0,
			wantM:   metersPerDegreeLat,
			epsilon: 1.0, // within 1 meter
		},
		{
			name: "equator 1 degree longitude",
			lat1: 0, lon1: 0,
			lat2: 0, lon2: 1,
			wantM:   metersPerDegreeLat,
			epsilon: 1.0,
		},
		{
			// Independent reference: R * c with c = 2*atan2(sqrt(a), sqrt(1-a)),
			// a = sin²(Δφ/2) + cos φ1 cos φ2 sin²(Δλ/2) for (0,0)→(10,20).
			name: "precomputed haversine (0,0) to (10,20)",
			lat1: 0, lon1: 0,
			lat2: 10, lon2: 20,
			wantM:   2476171.4106209576,
			epsilon: 1e-6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Distance(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if !approxEqual(got, tt.wantM, tt.epsilon) {
				t.Fatalf("Distance = %v, want ~%v (±%v)", got, tt.wantM, tt.epsilon)
			}
		})
	}
}

func TestDistance_Symmetric(t *testing.T) {
	a := Distance(40.0, -74.0, 34.0, -118.0)
	b := Distance(34.0, -118.0, 40.0, -74.0)
	if !approxEqual(a, b, 1e-9) {
		t.Fatalf("distance not symmetric: %v vs %v", a, b)
	}
}

func TestDistance_PositiveForSeparatedPoints(t *testing.T) {
	d := Distance(0, 0, 10, 10)
	if d <= 0 {
		t.Fatalf("expected positive distance, got %v", d)
	}
	// Half circumference is π*R ≈ 20e6 m; 10° hop is well below that.
	if d > math.Pi*EarthRadius {
		t.Fatalf("distance %v exceeds half circumference", d)
	}
}

func TestBoundingBox_Equator(t *testing.T) {
	min, max := BoundingBox([2]float64{0, 0}, 100000)
	wantMin := [2]float64{-0.8993216059187304, -0.8993216059187304}
	wantMax := [2]float64{0.8993216059187304, 0.8993216059187304}
	if !approxEqual(min[0], wantMin[0], 1e-6) || !approxEqual(min[1], wantMin[1], 1e-6) {
		t.Fatalf("min = %v, want %v", min, wantMin)
	}
	if !approxEqual(max[0], wantMax[0], 1e-6) || !approxEqual(max[1], wantMax[1], 1e-6) {
		t.Fatalf("max = %v, want %v", max, wantMax)
	}
}

func TestBoundingBox_45Degrees(t *testing.T) {
	min, max := BoundingBox([2]float64{45, 0}, 100000)
	wantMin := [2]float64{44.10067839408127, -1.2718328120254205}
	wantMax := [2]float64{45.89932160591873, 1.2718328120254205}
	if !approxEqual(min[0], wantMin[0], 1e-6) || !approxEqual(min[1], wantMin[1], 1e-6) {
		t.Fatalf("min = %v, want %v", min, wantMin)
	}
	if !approxEqual(max[0], wantMax[0], 1e-6) || !approxEqual(max[1], wantMax[1], 1e-6) {
		t.Fatalf("max = %v, want %v", max, wantMax)
	}
}

func TestBoundingBox_ZeroRadius(t *testing.T) {
	center := [2]float64{12.34, 56.78}
	min, max := BoundingBox(center, 0)
	if min != center || max != center {
		t.Fatalf("zero radius: min=%v max=%v, want both %v", min, max, center)
	}
}

func TestBoundingBox_ContainsCenter(t *testing.T) {
	center := [2]float64{51.5, -0.12}
	min, max := BoundingBox(center, 50000)
	if center[0] < min[0] || center[0] > max[0] || center[1] < min[1] || center[1] > max[1] {
		t.Fatalf("center %v not inside box min=%v max=%v", center, min, max)
	}
}

func TestBoundingBox_LonDeltaGrowsTowardPoles(t *testing.T) {
	// Longitude half-width should be larger at higher latitude for same radius.
	_, maxEq := BoundingBox([2]float64{0, 0}, 100000)
	_, maxHigh := BoundingBox([2]float64{60, 0}, 100000)
	deltaLonEq := maxEq[1]
	deltaLonHigh := maxHigh[1]
	if deltaLonHigh <= deltaLonEq {
		t.Fatalf("expected larger lon delta at 60° than equator: eq=%v high=%v", deltaLonEq, deltaLonHigh)
	}
}

func TestEarthRadius(t *testing.T) {
	if EarthRadius != 6371000.0 {
		t.Fatalf("EarthRadius = %v, want 6371000", EarthRadius)
	}
}

func TestDistanceSq_ApproxMatchesHaversineLocally(t *testing.T) {
	// Within ~10 km, equirectangular should be within a few meters of haversine.
	lat1, lon1 := 34.0, -118.0
	lat2, lon2 := 34.05, -118.04
	hav := Distance(lat1, lon1, lat2, lon2)
	approx := ApproxDistance(lat1, lon1, lat2, lon2)
	if !approxEqual(hav, approx, 5.0) {
		t.Fatalf("local approx %v vs haversine %v", approx, hav)
	}
	if DistanceSq(lat1, lon1, lat2, lon2) <= 0 {
		t.Fatal("DistanceSq should be positive")
	}
}

func TestAABBOverlap(t *testing.T) {
	if !AABBOverlap([2]float64{0, 0}, [2]float64{1, 1}, [2]float64{0.5, 0.5}, [2]float64{2, 2}) {
		t.Fatal("expected overlap")
	}
	if AABBOverlap([2]float64{0, 0}, [2]float64{1, 1}, [2]float64{2, 2}, [2]float64{3, 3}) {
		t.Fatal("expected no overlap")
	}
}

func TestQuantizeCenter(t *testing.T) {
	q := QuantizeCenter([2]float64{34.0004, -118.0006}, 0.001)
	if q[0] != 34.0 || q[1] != -118.001 {
		// -118.0006 / 0.001 = -118000.6 → round → -118001 → *0.001 = -118.001
		t.Fatalf("QuantizeCenter = %v", q)
	}
}

func TestQuantizeDeg_NonPositiveQuantumPassthrough(t *testing.T) {
	// quantumDeg <= 0 must return v unchanged (no divide-by-zero / Inf).
	for _, q := range []float64{0, -0.001, -1} {
		got := QuantizeDeg(12.345, q)
		if got != 12.345 {
			t.Fatalf("QuantizeDeg(12.345, %v) = %v, want 12.345", q, got)
		}
	}
	// Positive quantum still quantizes.
	if got := QuantizeDeg(1.2345, 0.01); !approxEqual(got, 1.23, 1e-12) {
		t.Fatalf("QuantizeDeg positive quantum = %v, want 1.23", got)
	}
}

func TestCellIndex(t *testing.T) {
	// 0.25° cells: floor division into int32 indices.
	if got := CellIndex(0, 0.25); got != 0 {
		t.Fatalf("CellIndex(0) = %d", got)
	}
	if got := CellIndex(0.24, 0.25); got != 0 {
		t.Fatalf("CellIndex(0.24) = %d", got)
	}
	if got := CellIndex(0.25, 0.25); got != 1 {
		t.Fatalf("CellIndex(0.25) = %d", got)
	}
	if got := CellIndex(-0.01, 0.25); got != -1 {
		t.Fatalf("CellIndex(-0.01) = %d, want -1", got)
	}
	// Non-positive cellDeg falls back to DefaultGridCellDeg.
	if got := CellIndex(0.3, 0); got != CellIndex(0.3, DefaultGridCellDeg) {
		t.Fatalf("CellIndex default cellDeg mismatch: %d vs %d", got, CellIndex(0.3, DefaultGridCellDeg))
	}
	if got := CellIndex(0.3, -1); got != CellIndex(0.3, DefaultGridCellDeg) {
		t.Fatalf("CellIndex negative cellDeg mismatch")
	}
}

func TestCellCover_SingleCell(t *testing.T) {
	// Point-like AABB entirely inside one 0.25° cell.
	min := [2]float64{34.01, -118.02}
	max := [2]float64{34.02, -118.01}
	cells := CellCover(min, max, 0.25, nil)
	if len(cells) != 1 {
		t.Fatalf("want 1 cell, got %d: %v", len(cells), cells)
	}
	want := CellKey{ILat: CellIndex(34.01, 0.25), ILon: CellIndex(-118.02, 0.25)}
	if cells[0] != want {
		t.Fatalf("cell = %+v, want %+v", cells[0], want)
	}
}

func TestCellCover_MultiCellAndSwap(t *testing.T) {
	// min/max deliberately swapped — CellCover should normalize.
	// 0–1° @ 0.5°: floor(0)=0 … floor(1)=2 → 3×3 = 9 cells.
	min := [2]float64{1.0, 1.0} // actually larger
	max := [2]float64{0.0, 0.0} // actually smaller
	cells := CellCover(min, max, 0.5, nil)
	if len(cells) != 9 {
		t.Fatalf("want 9 cells for 0–1° @ 0.5°, got %d: %v", len(cells), cells)
	}
	seen := map[CellKey]bool{}
	for _, c := range cells {
		seen[c] = true
	}
	for _, lat := range []int32{0, 1, 2} {
		for _, lon := range []int32{0, 1, 2} {
			if !seen[CellKey{ILat: lat, ILon: lon}] {
				t.Fatalf("missing cell {%d,%d}", lat, lon)
			}
		}
	}
}

func TestCellCover_DefaultCellDegAndAppend(t *testing.T) {
	dst := []CellKey{{ILat: 99, ILon: 99}}
	out := CellCover([2]float64{0, 0}, [2]float64{0.1, 0.1}, 0, dst)
	if len(out) < 2 {
		t.Fatalf("expected append onto dst, got %v", out)
	}
	if out[0] != (CellKey{ILat: 99, ILon: 99}) {
		t.Fatalf("dst prefix lost: %v", out)
	}
}

func TestCellCover_LatClampAndNonFinite(t *testing.T) {
	// Latitude below -90 / above 90 is clamped.
	cells := CellCover([2]float64{-100, 0}, [2]float64{100, 0.1}, 10, nil)
	if len(cells) == 0 {
		t.Fatal("expected cells after lat clamp")
	}
	// Non-finite lat → full -90..90 band.
	infCells := CellCover([2]float64{math.Inf(-1), 0}, [2]float64{math.Inf(1), 0.1}, 45, nil)
	if len(infCells) == 0 {
		t.Fatal("expected cells for non-finite lat")
	}
	// Non-finite lon spans mid=0 with ±90° cap.
	lonInf := CellCover([2]float64{0, math.NaN()}, [2]float64{0.1, math.Inf(1)}, 30, nil)
	if len(lonInf) == 0 {
		t.Fatal("expected cells for non-finite lon")
	}
	// Absurdly wide finite lon span is capped around midpoint (finite mid path).
	wide := CellCover([2]float64{0, -170}, [2]float64{0.1, 170}, 30, nil)
	if len(wide) == 0 {
		t.Fatal("expected cells for wide lon")
	}
	// Wide finite lon with NaN mid fallback when only one side finite:
	// both Inf/NaN lon already covered; one more path: both finite but > maxLonSpan
	// is the wide case above. Cover NaN lat with finite lon separately.
	nanLat := CellCover([2]float64{math.NaN(), -1}, [2]float64{math.NaN(), 1}, 1, nil)
	if len(nanLat) == 0 {
		t.Fatal("expected cells for NaN lat")
	}
}

func TestCellCover_AxisCap(t *testing.T) {
	// Force more than maxCellsPerAxis (128) along lon with a tiny cell size.
	cells := CellCover([2]float64{0, -40}, [2]float64{0.01, 40}, 0.1, nil)
	// After cap: at most 129 cells per axis (mid ± 64), so ≤ 129*129 but lat is 1 cell.
	// lat span tiny → 1 lat cell; lon capped to ≤ 129.
	lats := map[int32]bool{}
	lons := map[int32]bool{}
	for _, c := range cells {
		lats[c.ILat] = true
		lons[c.ILon] = true
	}
	if len(lons) > 129 {
		t.Fatalf("lon axis not capped: %d distinct lon indices", len(lons))
	}
	// Huge lat span with tiny cells also caps.
	latHuge := CellCover([2]float64{-80, 0}, [2]float64{80, 0.01}, 0.1, nil)
	lats2 := map[int32]bool{}
	for _, c := range latHuge {
		lats2[c.ILat] = true
	}
	if len(lats2) > 129 {
		t.Fatalf("lat axis not capped: %d", len(lats2))
	}
}

func BenchmarkDistance(b *testing.B) {
	const numPairs = 1024 * 64
	lats1 := make([]float64, numPairs)
	lons1 := make([]float64, numPairs)
	lats2 := make([]float64, numPairs)
	lons2 := make([]float64, numPairs)

	r := rand.New(rand.NewSource(42))
	for i := 0; i < numPairs; i++ {
		lats1[i] = -90 + r.Float64()*180
		lons1[i] = -180 + r.Float64()*360
		lats2[i] = -90 + r.Float64()*180
		lons2[i] = -180 + r.Float64()*360
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % numPairs
		_ = Distance(lats1[idx], lons1[idx], lats2[idx], lons2[idx])
	}
}

func BenchmarkBoundingBox(b *testing.B) {
	centers := [][2]float64{
		{0, 0},
		{45, -120},
		{-33.8, 151.2},
		{51.5, -0.12},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := centers[i%len(centers)]
		_, _ = BoundingBox(c, 100000)
	}
}

func BenchmarkDistanceSq(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = DistanceSq(34.0, -118.0, 34.1, -118.1)
	}
}
