// Package geo provides pure geographic helpers (haversine distance and
// axis-aligned bounding boxes). It depends only on the Go standard library.
package geo

import "math"

// EarthRadius is the approximate mean radius of Earth in meters.
const EarthRadius = 6371000.0

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

// BoundingBox returns an axis-aligned lat/lon bounding box (degrees) around
// center for the given radius in meters. center is [lat, lon].
//
// The box uses a simple equirectangular approximation (same math historically
// used by the FSD postoffice geospatial index).
func BoundingBox(center [2]float64, radiusM float64) (min, max [2]float64) {
	latRad := center[0] * degToRad
	const metersPerDegreeLat = (math.Pi * EarthRadius) / 180
	deltaLat := radiusM / metersPerDegreeLat
	metersPerDegreeLon := metersPerDegreeLat * math.Cos(latRad)
	deltaLon := radiusM / metersPerDegreeLon

	minLat := center[0] - deltaLat
	maxLat := center[0] + deltaLat
	minLon := center[1] - deltaLon
	maxLon := center[1] + deltaLon

	min = [2]float64{minLat, minLon}
	max = [2]float64{maxLat, maxLon}
	return min, max
}
