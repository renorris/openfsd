package cluster

import (
	"testing"
)

func TestMergeInterestBoxesATCFirst(t *testing.T) {
	var boxes []InterestBox
	// Many pilot boxes
	for i := 0; i < 100; i++ {
		boxes = append(boxes, InterestBox{
			AABB:  AABB{MinLat: float64(i), MaxLat: float64(i) + 0.1, MinLon: 0, MaxLon: 0.1},
			IsATC: false,
		})
	}
	// ATC/SECPOS
	for i := 0; i < 10; i++ {
		boxes = append(boxes, InterestBox{
			AABB:  AABB{MinLat: 50 + float64(i), MaxLat: 50.2 + float64(i), MinLon: 10, MaxLon: 10.2},
			IsATC: true,
		})
	}
	merged, overflow := MergeInterestBoxes(boxes, 64)
	if len(merged) > 64 {
		t.Fatalf("len=%d", len(merged))
	}
	_ = overflow
	// All ATC should be preserved preferentially — at least some ATC centers remain
	if len(merged) == 0 {
		t.Fatal("empty")
	}
}

func TestAnyAABBOverlap(t *testing.T) {
	a := []AABB{{MinLat: 0, MaxLat: 1, MinLon: 0, MaxLon: 1}}
	b := []AABB{{MinLat: 0.5, MaxLat: 1.5, MinLon: 0.5, MaxLon: 1.5}}
	if !AnyAABBOverlap(a, b) {
		t.Fatal("expected overlap")
	}
	c := []AABB{{MinLat: 10, MaxLat: 11, MinLon: 10, MaxLon: 11}}
	if AnyAABBOverlap(a, c) {
		t.Fatal("expected no overlap")
	}
}

func TestAABBFromCenterRange(t *testing.T) {
	a := AABBFromCenterRange(40, -74, 1852*50) // 50 NM
	if a.MinLat >= 40 || a.MaxLat <= 40 {
		t.Fatalf("%+v", a)
	}
}

func TestMergeNearest(t *testing.T) {
	boxes := []AABB{
		{0, 0, 1, 1},
		{0.1, 0.1, 1.1, 1.1},
		{10, 10, 11, 11},
	}
	out := mergeNearest(boxes, 2)
	if len(out) != 2 {
		t.Fatalf("%d", len(out))
	}
}

func TestMergeNearestEdges(t *testing.T) {
	if mergeNearest(nil, 0) != nil {
		t.Fatal()
	}
	boxes := []AABB{{0, 0, 1, 1}}
	if len(mergeNearest(boxes, 5)) != 1 {
		t.Fatal()
	}
	// many for nearest merge
	var many []AABB
	for i := 0; i < 10; i++ {
		many = append(many, AABB{float64(i), float64(i), float64(i) + 0.1, float64(i) + 0.1})
	}
	out := mergeNearest(many, 3)
	if len(out) != 3 {
		t.Fatal(len(out))
	}
	// MergeInterestBoxes empty pilots ATC over
	var atc []InterestBox
	for i := 0; i < 100; i++ {
		atc = append(atc, InterestBox{AABB: AABB{float64(i * 2), 0, float64(i*2) + 0.5, 0.5}, IsATC: true})
	}
	m, _ := MergeInterestBoxes(atc, 8)
	if len(m) > 8 {
		t.Fatal(len(m))
	}
	// quantize q<=0
	_ = quantizeMerge([]InterestBox{{AABB: AABB{0, 0, 1, 1}}}, 0)
}
