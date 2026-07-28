package afv

import (
	"math"
	"strings"

	"github.com/renorris/openfsd/internal/geo"
)

// FrequencyUnicomHz is 122.800 MHz in Hz.
const FrequencyUnicomHz uint32 = 122800000

// MetersPerNM is the international nautical mile in meters.
const MetersPerNM = 1852.0

// RangeClass selects max range for a radio.
type RangeClass int

const (
	RangeClassDefault RangeClass = iota
	RangeClassUnicom
	RangeClassATC
)

// IsATC reports whether a voice session should use ATC range.
// P0: len(transceivers) >= 2 OR callsign contains '_'.
func IsATC(callsign string, transceiverCount int) bool {
	if transceiverCount >= 2 {
		return true
	}
	return strings.Contains(callsign, "_")
}

// ClassifyRange returns the range class for a transmitter.
func ClassifyRange(freqHz uint32, isATC bool) RangeClass {
	if freqHz == FrequencyUnicomHz {
		return RangeClassUnicom
	}
	if isATC {
		return RangeClassATC
	}
	return RangeClassDefault
}

// MaxRangeNM returns configured max range for a class.
func (c *Config) MaxRangeNM(class RangeClass) float64 {
	if c == nil {
		switch class {
		case RangeClassUnicom:
			return 15
		case RangeClassATC:
			return 150
		default:
			return 40
		}
	}
	switch class {
	case RangeClassUnicom:
		if c.RangeUnicomNM > 0 {
			return c.RangeUnicomNM
		}
		return 15
	case RangeClassATC:
		if c.RangeATCNM > 0 {
			return c.RangeATCNM
		}
		return 150
	default:
		if c.RangeDefaultNM > 0 {
			return c.RangeDefaultNM
		}
		return 40
	}
}

// EdgeRatio returns DistanceRatio at max range (default 0.1).
func (c *Config) EdgeRatio() float64 {
	if c == nil || c.RangeEdgeRatio <= 0 || c.RangeEdgeRatio >= 1 {
		return 0.1
	}
	return c.RangeEdgeRatio
}

// DistanceRatio computes 0..1 reception ratio. Returns ok=false if out of range.
// At distance 0 → 1.0; at maxRange → edgeRatio.
func DistanceRatio(distM, maxRangeM, edgeRatio float64) (ratio float32, ok bool) {
	if maxRangeM <= 0 || math.IsNaN(distM) || math.IsInf(distM, 0) {
		return 0, false
	}
	if distM < 0 {
		distM = 0
	}
	if distM > maxRangeM {
		return 0, false
	}
	if edgeRatio <= 0 || edgeRatio >= 1 {
		edgeRatio = 0.1
	}
	// linear: 1.0 at 0, edgeRatio at maxRange
	r := 1.0 - (distM/maxRangeM)*(1.0-edgeRatio)
	if r < edgeRatio {
		r = edgeRatio
	}
	if r > 1 {
		r = 1
	}
	return float32(r), true
}

// SlantRangeM is equirectangular ground distance (P0 free-space; altitude ignored for range check).
func SlantRangeM(lat1, lon1, lat2, lon2 float64) float64 {
	return geo.ApproxDistance(lat1, lon1, lat2, lon2)
}

// NMToMeters converts nautical miles to meters.
func NMToMeters(nm float64) float64 {
	return nm * MetersPerNM
}
