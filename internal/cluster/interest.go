package cluster

import (
	"math"
	"sort"

	"github.com/renorris/openfsd/internal/geo"
)

// MaxInterestBoxes is the hard cap for InterestSummary (design default 64).
const MaxInterestBoxes = 64

// InterestBox is a classified AABB for merge policy.
type InterestBox struct {
	AABB
	IsATC bool // true for ATC primary + SECPOS
}

// MergeInterestBoxes implements the PR-8a ATC-first merge / quantize policy.
// Returns merged boxes ≤ maxBoxes (default MaxInterestBoxes).
func MergeInterestBoxes(boxes []InterestBox, maxBoxes int) (merged []AABB, overflow bool) {
	if maxBoxes <= 0 {
		maxBoxes = MaxInterestBoxes
	}
	if len(boxes) == 0 {
		return nil, false
	}
	if len(boxes) <= maxBoxes {
		return filterToAABB(boxes), false
	}

	// Prefer ATC: take ATC boxes first (quantize-merged), then pilots into remainder.
	atcRaw := filterClass(boxes, true)
	pilotRaw := filterClass(boxes, false)

	q := 0.05
	atc := quantizeMerge(atcRaw, q)
	// Grow q until ATC fits in maxBoxes (last resort nearest-merge).
	for step := 0; step < 8 && len(atc) > maxBoxes; step++ {
		q *= 2
		atc = quantizeMerge(atcRaw, q)
	}
	if len(atc) > maxBoxes {
		atc = mergeNearest(atc, maxBoxes)
		overflow = true
		return atc, overflow
	}

	remain := maxBoxes - len(atc)
	if remain <= 0 || len(pilotRaw) == 0 {
		return atc, len(pilotRaw) > 0
	}
	pq := 0.05
	pilot := quantizeMerge(pilotRaw, pq)
	for step := 0; step < 8 && len(pilot) > remain; step++ {
		pq *= 2
		pilot = quantizeMerge(pilotRaw, pq)
	}
	if len(pilot) > remain {
		pilot = mergeNearest(pilot, remain)
	}
	return append(atc, pilot...), false
}

func filterClass(boxes []InterestBox, atc bool) []InterestBox {
	var out []InterestBox
	for _, b := range boxes {
		if b.IsATC == atc {
			out = append(out, b)
		}
	}
	return out
}

func filterToAABB(boxes []InterestBox) []AABB {
	out := make([]AABB, len(boxes))
	for i, b := range boxes {
		out[i] = b.AABB
	}
	return out
}

func quantizeMerge(boxes []InterestBox, q float64) []AABB {
	if len(boxes) == 0 {
		return nil
	}
	if q <= 0 {
		q = 0.05
	}
	type cellKey struct{ x, y int }
	cells := map[cellKey]AABB{}
	for _, b := range boxes {
		cx := (b.MinLat + b.MaxLat) / 2
		cy := (b.MinLon + b.MaxLon) / 2
		k := cellKey{x: int(math.Floor(cx / q)), y: int(math.Floor(cy / q))}
		if cur, ok := cells[k]; ok {
			cells[k] = unionAABB(cur, b.AABB)
		} else {
			cells[k] = b.AABB
		}
	}
	out := make([]AABB, 0, len(cells))
	for _, a := range cells {
		out = append(out, a)
	}
	return out
}

func unionAABB(a, b AABB) AABB {
	return AABB{
		MinLat: math.Min(a.MinLat, b.MinLat),
		MinLon: math.Min(a.MinLon, b.MinLon),
		MaxLat: math.Max(a.MaxLat, b.MaxLat),
		MaxLon: math.Max(a.MaxLon, b.MaxLon),
	}
}

// mergeNearest reduces boxes to at most n by repeatedly merging closest centers.
func mergeNearest(boxes []AABB, n int) []AABB {
	if n <= 0 {
		return nil
	}
	if len(boxes) <= n {
		return boxes
	}
	out := append([]AABB(nil), boxes...)
	for len(out) > n {
		bestI, bestJ := 0, 1
		bestD := math.MaxFloat64
		for i := 0; i < len(out); i++ {
			ci := centerOf(out[i])
			for j := i + 1; j < len(out); j++ {
				cj := centerOf(out[j])
				d := (ci[0]-cj[0])*(ci[0]-cj[0]) + (ci[1]-cj[1])*(ci[1]-cj[1])
				if d < bestD {
					bestD = d
					bestI, bestJ = i, j
				}
			}
		}
		merged := unionAABB(out[bestI], out[bestJ])
		if bestJ < bestI {
			bestI, bestJ = bestJ, bestI
		}
		out = append(out[:bestJ], out[bestJ+1:]...)
		out = append(out[:bestI], out[bestI+1:]...)
		out = append(out, merged)
	}
	return out
}

func centerOf(a AABB) [2]float64 {
	return [2]float64{(a.MinLat + a.MaxLat) / 2, (a.MinLon + a.MaxLon) / 2}
}

// AnyAABBOverlap reports whether any box in a overlaps any box in b.
func AnyAABBOverlap(a, b []AABB) bool {
	for i := range a {
		for j := range b {
			if geo.AABBOverlap(
				[2]float64{a[i].MinLat, a[i].MinLon},
				[2]float64{a[i].MaxLat, a[i].MaxLon},
				[2]float64{b[j].MinLat, b[j].MinLon},
				[2]float64{b[j].MaxLat, b[j].MaxLon},
			) {
				return true
			}
		}
	}
	return false
}

// AABBFromCenterRange builds an AABB from lat/lon center and range in meters.
func AABBFromCenterRange(lat, lon, rangeM float64) AABB {
	dLat := rangeM / 111320.0
	cosLat := math.Cos(lat * math.Pi / 180)
	if cosLat < 0.01 {
		cosLat = 0.01
	}
	dLon := rangeM / (111320.0 * cosLat)
	return AABB{
		MinLat: lat - dLat,
		MaxLat: lat + dLat,
		MinLon: lon - dLon,
		MaxLon: lon + dLon,
	}
}

// SortAABBsForTest sorts for stable golden comparison.
func SortAABBsForTest(boxes []AABB) {
	sort.Slice(boxes, func(i, j int) bool {
		if boxes[i].MinLat != boxes[j].MinLat {
			return boxes[i].MinLat < boxes[j].MinLat
		}
		return boxes[i].MinLon < boxes[j].MinLon
	})
}
