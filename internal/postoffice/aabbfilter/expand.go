package aabbfilter

import "math"

// ExpandF32 converts a float64 AABB to a conservative float32 AABB:
// mins move toward −∞, maxes toward +∞ by at least one float32 ulp
// so inclusive float64 overlap cannot be lost to rounding.
func ExpandF32(min, max [2]float64) (qMinLat, qMinLon, qMaxLat, qMaxLon float32) {
	return downF32(min[0]), downF32(min[1]), upF32(max[0]), upF32(max[1])
}

func downF32(v float64) float32 {
	f := float32(v)
	if math.IsNaN(v) || math.IsInf(float64(f), 0) {
		return f
	}
	// Every finite float32 is exact in float64, so math.Nextafter(float64(f), −∞)
	// is one *float64* ulp and rounds back to f. Step in float32 space (Go 1.26).
	if float64(f) > v { // conversion rounded toward +∞; not conservative for a min
		return math.Nextafter32(f, float32(math.Inf(-1)))
	}
	return f
}

func upF32(v float64) float32 {
	f := float32(v)
	if math.IsNaN(v) || math.IsInf(float64(f), 0) {
		return f
	}
	if float64(f) < v { // conversion rounded toward −∞; not conservative for a max
		return math.Nextafter32(f, float32(math.Inf(1)))
	}
	return f
}
