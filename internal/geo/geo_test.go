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
