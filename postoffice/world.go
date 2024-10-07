package postoffice

// World holds a bucket for every 15-bit precision geohash,
// i.e. splitting the world into 2^15 buckets.
type World struct {
	buckets [32768]GeohashBucket
}

func NewWorld() *World {
	buckets := [32768]GeohashBucket{}

	// Initialize all buckets
	for i := 0; i < len(buckets); i++ {
		buckets[i] = GeohashBucket{
			addresses: make([]Address, 0),
		}
	}

	return &World{buckets: buckets}
}

// Bucket returns the geohash bucket for `index` (15-bit geohash)
func (w *World) Bucket(index uint64) *GeohashBucket {
	if index < 0 || index >= uint64(len(w.buckets)) {
		panic("geohash bucket index out of bounds")
	}

	return &w.buckets[index]
}
