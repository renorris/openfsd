// Package geo provides pure geographic helpers (haversine distance and
// axis-aligned bounding boxes). It depends only on the Go standard library.
package geo

import "math"

// EarthRadius is the approximate mean radius of Earth in meters.
const EarthRadius = 6371000.0

// MetersPerDegreeLat is the equirectangular meters-per-degree of latitude
// at the mean Earth radius (πR/180).
const MetersPerDegreeLat = (math.Pi * EarthRadius) / 180

const degToRad = math.Pi / 180

// Distance returns the great-circle distance in meters between two points
// specified as latitude/longitude in degrees, using the Haversine formula.
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * degToRad
	dLon := (lon2 - lon1) * degToRad

	sinDLat2 := math.Sin(dLat * 0.5)
	sinDLon2 := math.Sin(dLon * 0.5)

	cosLat1 := math.Cos(lat1 * degToRad)
	cosLat2 := math.Cos(lat2 * degToRad)

	a := sinDLat2*sinDLat2 + cosLat1*cosLat2*sinDLon2*sinDLon2

	sqrtA := math.Sqrt(a)
	sqrt1MinusA := math.Sqrt(1 - a)

	c := 2 * math.Atan2(sqrtA, sqrt1MinusA)

	return EarthRadius * c
}

// DistanceSq returns the squared equirectangular distance in meters² between
// two points (degrees). Prefer this over Distance when only comparing or
// thresholding ranges (avoids sqrt/trig of haversine). Accurate enough for
// local FSD visibility and send-fast hysteresis (tens of NM).
//
// The projection is centered on lat1 (same convention as BoundingBox).
func DistanceSq(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * MetersPerDegreeLat
	cosLat := math.Cos(lat1 * degToRad)
	dLon := (lon2 - lon1) * MetersPerDegreeLat * cosLat
	return dLat*dLat + dLon*dLon
}

// ApproxDistance returns equirectangular distance in meters (sqrt of DistanceSq).
func ApproxDistance(lat1, lon1, lat2, lon2 float64) float64 {
	return math.Sqrt(DistanceSq(lat1, lon1, lat2, lon2))
}

// BoundingBox returns an axis-aligned lat/lon bounding box (degrees) around
// center for the given radius in meters. center is [lat, lon] in degrees;
// radiusM is meters.
//
// The box uses a simple equirectangular approximation (same math historically
// used by the FSD postoffice geospatial index). Longitude half-width is
// radiusM / (metersPerDegreeLat * cos(lat)). Near the poles (|lat| → 90°),
// cos(lat) → 0 so deltaLon grows without bound (and is Inf at exactly ±90°).
// Callers that index polar positions should treat the result as a coarse
// filter only; this API intentionally does not clamp latitude or cap deltaLon.
func BoundingBox(center [2]float64, radiusM float64) (min, max [2]float64) {
	latRad := center[0] * degToRad
	deltaLat := radiusM / MetersPerDegreeLat
	metersPerDegreeLon := MetersPerDegreeLat * math.Cos(latRad)
	deltaLon := radiusM / metersPerDegreeLon

	minLat := center[0] - deltaLat
	maxLat := center[0] + deltaLat
	minLon := center[1] - deltaLon
	maxLon := center[1] + deltaLon

	min = [2]float64{minLat, minLon}
	max = [2]float64{maxLat, maxLon}
	return min, max
}

// AABBOverlap reports whether two axis-aligned boxes [min,max] inclusive overlap.
func AABBOverlap(minA, maxA, minB, maxB [2]float64) bool {
	return minA[0] <= maxB[0] && maxA[0] >= minB[0] &&
		minA[1] <= maxB[1] && maxA[1] >= minB[1]
}

// QuantizeDeg quantizes a lat/lon degree value to a fixed grid.
// quantumDeg is the cell size in degrees (e.g. 0.001° ≈ 111 m of latitude).
func QuantizeDeg(v, quantumDeg float64) float64 {
	if quantumDeg <= 0 {
		return v
	}
	return math.Round(v/quantumDeg) * quantumDeg
}

// QuantizeCenter quantizes a [lat, lon] center for spatial-index keys so tiny
// movements do not thrash Delete+Insert. Default quantum is ~100 m of latitude.
const DefaultIndexQuantumDeg = 0.001 // ≈ 111 m

func QuantizeCenter(center [2]float64, quantumDeg float64) [2]float64 {
	return [2]float64{
		QuantizeDeg(center[0], quantumDeg),
		QuantizeDeg(center[1], quantumDeg),
	}
}
