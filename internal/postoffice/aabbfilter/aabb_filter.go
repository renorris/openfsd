// Package aabbfilter is a leaf scalar AABB overlap scan used by postoffice
// Search. It must not import session or the parent postoffice package.
package aabbfilter

// FilterOverlap appends indices i in [0, n) where live[i] != 0 and
// query overlaps the AABB at i. Hit order is strictly increasing.
//
// query is [minLat, minLon, maxLat, maxLon] float32 (already ExpandF32'd).
// Inclusive inequalities, same as geo.AABBOverlap / geo.AABBOverlapF32.
//
// Length policy (never panic): n = min of the five lens; 0 or nil → return dst.
// Extra tail of a longer slice is ignored.
// If cap(dst) < n, allocate cap n (callers SHOULD pass cap >= n).
func FilterOverlap(query [4]float32, minLat, minLon, maxLat, maxLon []float32, live []byte, dst []int32) []int32 {
	return FilterOverlapScalar(query, minLat, minLon, maxLat, maxLon, live, dst)
}

// FilterOverlapSIMD is the optional assembly kernel. Nil in the scalar product.
var FilterOverlapSIMD func(query [4]float32, minLat, minLon, maxLat, maxLon []float32, live []byte, dst []int32) []int32

// Enabled reports whether a SIMD kernel is wired. Always false in PR 2.
func Enabled() bool { return false }
