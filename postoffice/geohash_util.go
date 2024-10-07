package postoffice

import "github.com/mmcloughlin/geohash"

// Number of bits to encode for a full precision geohash.
// This is the bit-depth of the Geohash() value stored in each Address.
const geohashFullPrecisionBits = 30

// Number of bits to utilize for determining which bucket a geohash falls into.
const geohashBucketPrecisionBits = 15

// Bits of precision to use for general-proximity broadcasts
const geohashGeneralProximityPrecisionBits = geohashBucketPrecisionBits

// Bits of precision to use for close-proximity broadcasts
const geohashCloseProximityPrecisionBits = 25

type Geohash struct {
	val       uint64
	precision int
}

func NewGeohash(lat, lon float64) Geohash {
	return Geohash{
		val:       geohash.EncodeIntWithPrecision(lat, lon, geohashFullPrecisionBits),
		precision: geohashFullPrecisionBits,
	}
}

func NewGeohashManual(hash uint64, precision int) Geohash {
	return Geohash{
		val:       hash,
		precision: precision,
	}
}

func (g Geohash) Neighbors(precision int) []uint64 {
	return geohash.NeighborsIntWithPrecision(g.AsPrecision(precision), uint(precision))
}

func (g Geohash) Precision() int {
	return g.precision
}

func (g Geohash) Val() uint64 {
	return g.val
}

func (g Geohash) AsPrecision(precision int) uint64 {
	shift := g.precision - precision
	if shift <= 0 {
		return g.val
	}
	return g.val >> shift
}
