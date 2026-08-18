package aabbfilter

import (
	"math"
	"sync"
	"testing"

	"github.com/renorris/openfsd/internal/geo"
)

func TestEnabledFalse(t *testing.T) {
	if Enabled() {
		t.Fatal("Enabled must be false in scalar product")
	}
	if FilterOverlapSIMD != nil {
		t.Fatal("FilterOverlapSIMD must be nil in scalar product")
	}
}

func TestFilterOverlapScalar_Table(t *testing.T) {
	// One slot at index 0 unless noted.
	mk := func(minLat, minLon, maxLat, maxLon float32, live byte) (a, b, c, d []float32, l []byte) {
		return []float32{minLat}, []float32{minLon}, []float32{maxLat}, []float32{maxLon}, []byte{live}
	}
	q := func(minLat, minLon, maxLat, maxLon float32) [4]float32 {
		return [4]float32{minLat, minLon, maxLat, maxLon}
	}

	tests := []struct {
		name    string
		query   [4]float32
		minLat  []float32
		minLon  []float32
		maxLat  []float32
		maxLon  []float32
		live    []byte
		wantIdx []int32
	}{
		{
			name:    "overlap",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{0.5},
			minLon:  []float32{0.5},
			maxLat:  []float32{2},
			maxLon:  []float32{2},
			live:    []byte{1},
			wantIdx: []int32{0},
		},
		{
			name:    "inverted min>max",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{2},
			minLon:  []float32{0},
			maxLat:  []float32{1},
			maxLon:  []float32{1},
			live:    []byte{1},
			wantIdx: nil,
		},
		{
			name:    "live==2 still occupied",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{0},
			minLon:  []float32{0},
			maxLat:  []float32{1},
			maxLon:  []float32{1},
			live:    []byte{2},
			wantIdx: []int32{0},
		},
		{
			name:    "NaN query no hit",
			query:   q(float32(math.NaN()), 0, 1, 1),
			minLat:  []float32{0},
			minLon:  []float32{0},
			maxLat:  []float32{1},
			maxLon:  []float32{1},
			live:    []byte{1},
			wantIdx: nil,
		},
		{
			name:    "inclusive touch",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{1},
			minLon:  []float32{1},
			maxLat:  []float32{2},
			maxLon:  []float32{2},
			live:    []byte{1},
			wantIdx: []int32{0},
		},
		{
			name:    "disjoint",
			query:   q(-1, -1, 0, 0),
			minLat:  []float32{1},
			minLon:  []float32{1},
			maxLat:  []float32{2},
			maxLon:  []float32{2},
			live:    []byte{1},
			wantIdx: nil,
		},
		{
			name:    "NaN no hit",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{float32(math.NaN())},
			minLon:  []float32{0},
			maxLat:  []float32{1},
			maxLon:  []float32{1},
			live:    []byte{1},
			wantIdx: nil,
		},
		{
			name:    "+Inf extent overlaps finite query",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{float32(math.Inf(-1))},
			minLon:  []float32{float32(math.Inf(-1))},
			maxLat:  []float32{float32(math.Inf(1))},
			maxLon:  []float32{float32(math.Inf(1))},
			live:    []byte{1},
			wantIdx: []int32{0},
		},
		{
			name:    "polar-ish huge lon",
			query:   q(89, -10, 90, 10),
			minLat:  []float32{88},
			minLon:  []float32{-180},
			maxLat:  []float32{90},
			maxLon:  []float32{180},
			live:    []byte{1},
			wantIdx: []int32{0},
		},
		{
			name:    "denormal",
			query:   q(0, 0, math.SmallestNonzeroFloat32, math.SmallestNonzeroFloat32),
			minLat:  []float32{0},
			minLon:  []float32{0},
			maxLat:  []float32{math.SmallestNonzeroFloat32},
			maxLon:  []float32{math.SmallestNonzeroFloat32},
			live:    []byte{1},
			wantIdx: []int32{0},
		},
		{
			name:    "live==0 omitted",
			query:   q(0, 0, 1, 1),
			minLat:  []float32{0},
			minLon:  []float32{0},
			maxLat:  []float32{1},
			maxLon:  []float32{1},
			live:    []byte{0},
			wantIdx: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.minLat == nil {
				tt.minLat, tt.minLon, tt.maxLat, tt.maxLon, tt.live = mk(0, 0, 1, 1, 1)
			}
			got := FilterOverlapScalar(tt.query, tt.minLat, tt.minLon, tt.maxLat, tt.maxLon, tt.live, nil)
			if !int32sEqual(got, tt.wantIdx) {
				t.Fatalf("got %v want %v", got, tt.wantIdx)
			}
			// Public FilterOverlap must match scalar.
			got2 := FilterOverlap(tt.query, tt.minLat, tt.minLon, tt.maxLat, tt.maxLon, tt.live, nil)
			if !int32sEqual(got2, tt.wantIdx) {
				t.Fatalf("FilterOverlap got %v want %v", got2, tt.wantIdx)
			}
		})
	}

	// dst append: existing prefix is kept.
	dst := []int32{99}
	got := FilterOverlap([4]float32{0, 0, 1, 1}, []float32{0}, []float32{0}, []float32{1}, []float32{1}, []byte{1}, dst)
	if !int32sEqual(got, []int32{99, 0}) {
		t.Fatalf("dst append: got %v", got)
	}
}

func TestExpandF32_BarelyTouching(t *testing.T) {
	// Value just above the midpoint between two adjacent float32s so conversion
	// rounds toward +∞ (not float32-exact).
	f := float32(1.0)
	next := math.Nextafter32(f, 2)
	mid := (float64(f) + float64(next)) / 2
	v := math.Nextafter(mid, math.Inf(1))
	if float64(float32(v)) == v {
		t.Fatal("need a non-float32-exact edge")
	}
	if float64(float32(v)) <= v {
		t.Fatal("expected conversion to round toward +∞")
	}

	// Two float64 boxes that touch at v.
	aMin := [2]float64{0, 0}
	aMax := [2]float64{v, v}
	bMin := [2]float64{v, v}
	bMax := [2]float64{v + 10, v + 10}
	if !geo.AABBOverlap(aMin, aMax, bMin, bMax) {
		t.Fatal("float64 boxes must overlap")
	}

	// Naive F32 min of B rounded up; query max = previous float32 (f).
	// Inclusive AABBOverlapF32 misses; expandF32 of B's min steps down and hits.
	naiveBMin := float32(v)
	prev := math.Nextafter32(naiveBMin, float32(math.Inf(-1)))
	naiveHit := geo.AABBOverlapF32(
		[2]float32{naiveBMin, naiveBMin}, [2]float32{naiveBMin + 10, naiveBMin + 10},
		[2]float32{0, 0}, [2]float32{prev, prev},
	)
	if naiveHit {
		t.Fatal("expected naive (non-ulp) conversion to miss this barely-touching pair")
	}

	eMinLat, eMinLon, eMaxLat, eMaxLon := ExpandF32(bMin, bMax)
	expandHit := geo.AABBOverlapF32(
		[2]float32{eMinLat, eMinLon}, [2]float32{eMaxLat, eMaxLon},
		[2]float32{0, 0}, [2]float32{prev, prev},
	)
	if !expandHit {
		t.Fatal("expandF32 must hit after a real float32-ulp expand")
	}

	// Store+query both expanded still overlap.
	qa0, qa1, qa2, qa3 := ExpandF32(aMin, aMax)
	if !geo.AABBOverlapF32(
		[2]float32{qa0, qa1}, [2]float32{qa2, qa3},
		[2]float32{eMinLat, eMinLon}, [2]float32{eMaxLat, eMaxLon},
	) {
		t.Fatal("expanded pair must overlap")
	}

	// Twin: v just below midpoint so float32(v) < v (rounds toward −∞).
	vDown := math.Nextafter(mid, math.Inf(-1))
	if float64(float32(vDown)) == vDown {
		t.Fatal("need non-float32-exact max edge")
	}
	if float64(float32(vDown)) >= vDown {
		t.Fatal("expected conversion to round toward −∞")
	}
	naiveMax := float32(vDown)
	nextMax := math.Nextafter32(naiveMax, float32(math.Inf(1)))
	naiveMaxHit := geo.AABBOverlapF32(
		[2]float32{0, 0}, [2]float32{naiveMax, naiveMax},
		[2]float32{nextMax, nextMax}, [2]float32{nextMax + 10, nextMax + 10},
	)
	if naiveMaxHit {
		t.Fatal("expected naive max (rounded down) to miss")
	}
	_, _, eMaxLat2, eMaxLon2 := ExpandF32([2]float64{0, 0}, [2]float64{vDown, vDown})
	if !geo.AABBOverlapF32(
		[2]float32{0, 0}, [2]float32{eMaxLat2, eMaxLon2},
		[2]float32{nextMax, nextMax}, [2]float32{nextMax + 10, nextMax + 10},
	) {
		t.Fatal("upF32 must step toward +∞ so barely-touching max hits")
	}

	// NaN / Inf passthrough on both helpers.
	if !math.IsNaN(float64(downF32(math.NaN()))) {
		t.Fatal("downF32 NaN")
	}
	if !math.IsNaN(float64(upF32(math.NaN()))) {
		t.Fatal("upF32 NaN")
	}
	if !math.IsInf(float64(upF32(math.Inf(1))), 1) {
		t.Fatal("upF32 +Inf")
	}
	if !math.IsInf(float64(downF32(math.Inf(1))), 1) {
		t.Fatal("downF32 +Inf")
	}
	if !math.IsInf(float64(downF32(math.Inf(-1))), -1) {
		t.Fatal("downF32 -Inf")
	}
	if !math.IsInf(float64(upF32(math.Inf(-1))), -1) {
		t.Fatal("upF32 -Inf")
	}
}

func TestFilterOverlap_LengthPolicy(t *testing.T) {
	got := FilterOverlap([4]float32{}, nil, nil, nil, nil, nil, nil)
	if got != nil {
		t.Fatalf("n=0 nil dst: got %v", got)
	}
	dst := []int32{99}
	got = FilterOverlap([4]float32{0, 0, 1, 1}, nil, []float32{0}, []float32{1}, []float32{1}, []byte{1}, dst[:0])
	if len(got) != 0 {
		t.Fatalf("n=0 mismatched: got %v", got)
	}

	minLat := []float32{-1, -1, -1}
	minLon := []float32{-1, -1}
	maxLat := []float32{1, 1, 1, 1}
	maxLon := []float32{1, 1, 1}
	live := []byte{1, 1, 1}
	got = FilterOverlap([4]float32{-0.5, -0.5, 0.5, 0.5}, minLat, minLon, maxLat, maxLon, live, nil)
	if !int32sEqual(got, []int32{0, 1}) {
		t.Fatalf("mismatched lens n=min: got %v", got)
	}

	small := make([]int32, 0, 1)
	got = FilterOverlap([4]float32{-0.5, -0.5, 0.5, 0.5}, minLat, minLon, maxLat, maxLon, live, small)
	if cap(got) < 2 {
		t.Fatalf("cap < n should reallocate, cap=%d", cap(got))
	}
	if !int32sEqual(got, []int32{0, 1}) {
		t.Fatalf("after realloc: %v", got)
	}
}

func TestFilterOverlap_Sizes(t *testing.T) {
	sizes := []int{0, 1, 3, 4, 5, 7, 8, 15, 16, 17, 1024, 10000}
	for _, n := range sizes {
		minLat := make([]float32, n)
		minLon := make([]float32, n)
		maxLat := make([]float32, n)
		maxLon := make([]float32, n)
		live := make([]byte, n)
		var want []int32
		for i := 0; i < n; i++ {
			minLat[i], minLon[i] = -1, -1
			maxLat[i], maxLon[i] = 1, 1
			if i%3 != 0 {
				live[i] = 1
				want = append(want, int32(i))
			}
		}
		dst := make([]int32, 0, n)
		got := FilterOverlap([4]float32{-0.5, -0.5, 0.5, 0.5}, minLat, minLon, maxLat, maxLon, live, dst)
		if !int32sEqual(got, want) {
			t.Fatalf("n=%d got %v want %v", n, got, want)
		}
		// Strictly increasing.
		for i := 1; i < len(got); i++ {
			if got[i] <= got[i-1] {
				t.Fatalf("n=%d hit order not increasing: %v", n, got)
			}
		}
	}
}

func TestFilterOverlap_ConcurrentStores(t *testing.T) {
	const n = 256
	minLat := make([]float32, n)
	minLon := make([]float32, n)
	maxLat := make([]float32, n)
	maxLon := make([]float32, n)
	live := make([]byte, n)
	for i := 0; i < n; i++ {
		minLat[i], minLon[i] = -1, -1
		maxLat[i], maxLon[i] = 1, 1
		live[i] = 1
	}
	query := [4]float32{-0.5, -0.5, 0.5, 0.5}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				live[0] = 0
				minLat[0] = float32(math.NaN())
				minLon[0] = float32(math.NaN())
				maxLat[0] = float32(math.NaN())
				maxLon[0] = float32(math.NaN())
				minLat[0], minLon[0] = -1, -1
				maxLat[0], maxLon[0] = 1, 1
				live[0] = 1
			}
		}
	}()

	dst := make([]int32, 0, n)
	for i := 0; i < 2000; i++ {
		dst = FilterOverlap(query, minLat, minLon, maxLat, maxLon, live, dst[:0])
	}
	close(stop)
	wg.Wait()

	live[0] = 0
	minLat[0] = float32(math.NaN())
	got := FilterOverlap(query, minLat, minLon, maxLat, maxLon, live, dst[:0])
	for _, idx := range got {
		if idx == 0 {
			t.Fatal("tombstone index 0 should eventually be omitted")
		}
	}
}

func BenchmarkFilterOverlap(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run("n="+itoa(n), func(b *testing.B) {
			minLat := make([]float32, n)
			minLon := make([]float32, n)
			maxLat := make([]float32, n)
			maxLon := make([]float32, n)
			live := make([]byte, n)
			for i := 0; i < n; i++ {
				minLat[i], minLon[i] = -1, -1
				maxLat[i], maxLon[i] = 1, 1
				if i%10 != 0 {
					live[i] = 1
				}
			}
			query := [4]float32{-0.5, -0.5, 0.5, 0.5}
			dst := make([]int32, 0, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst = FilterOverlap(query, minLat, minLon, maxLat, maxLon, live, dst[:0])
			}
		})
	}
}

func int32sEqual(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
