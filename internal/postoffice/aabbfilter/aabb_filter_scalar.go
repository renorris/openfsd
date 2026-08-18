package aabbfilter

import "github.com/renorris/openfsd/internal/geo"

// FilterOverlapScalar is the portable overlap scan (spec). Always compiled.
//
// go:norace applies solely to the VisBox torn-read contract (concurrent
// ordinary float32 sidecar stores vs Search loads). It is not a license
// to share dst unsafely across goroutines.

//go:norace
func FilterOverlapScalar(query [4]float32, minLat, minLon, maxLat, maxLon []float32, live []byte, dst []int32) []int32 {
	n := len(minLat)
	if len(minLon) < n {
		n = len(minLon)
	}
	if len(maxLat) < n {
		n = len(maxLat)
	}
	if len(maxLon) < n {
		n = len(maxLon)
	}
	if len(live) < n {
		n = len(live)
	}
	if n == 0 {
		return dst
	}
	if cap(dst) < n {
		dst = make([]int32, 0, n)
	}
	qMinLat, qMinLon, qMaxLat, qMaxLon := query[0], query[1], query[2], query[3]
	for i := 0; i < n; i++ {
		if live[i] == 0 {
			continue
		}
		sMinLat := minLat[i]
		sMinLon := minLon[i]
		sMaxLat := maxLat[i]
		sMaxLon := maxLon[i]
		if geo.AABBOverlapF32(
			[2]float32{sMinLat, sMinLon},
			[2]float32{sMaxLat, sMaxLon},
			[2]float32{qMinLat, qMinLon},
			[2]float32{qMaxLat, qMaxLon},
		) {
			dst = append(dst, int32(i))
		}
	}
	return dst
}
