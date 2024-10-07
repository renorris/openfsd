package postoffice

func (p *PostOffice) sendDirectMail(mail *Mail) {
	addr, ok := p.addressRegistry.Load(mail.Recipient())
	if !ok {
		// Drop the message if no key is found
		return
	}

	addr.SendMail(mail.Packet())
}

func (p *PostOffice) sendBroadcastMail(mail *Mail) {
	p.addressRegistry.ForEach(func(_ string, addr Address) bool {
		// Skip self
		if addr == mail.Source() {
			return true
		}

		addr.SendMail(mail.Packet())
		return true
	})
}

func (p *PostOffice) sendSupervisorBroadcastMail(mail *Mail) {
	p.supervisorAddressRegistry.ForEach(func(_ string, addr Address) bool {
		// Skip ourselves
		if addr == mail.Source() {
			return true
		}

		addr.SendMail(mail.Packet())
		return true
	})
}

func (p *PostOffice) sendProximityMail(mail *Mail, precision int) {
	self := mail.Source()
	source := self.Geohash()

	// Send to our own bucket
	bucket := p.world.Bucket(source.AsPrecision(geohashBucketPrecisionBits))
	for _, addr := range bucket.RLock() {
		if addr == self {
			continue
		}
		if addr.Geohash().AsPrecision(precision) == source.AsPrecision(precision) {
			addr.SendMail(mail.Packet())
		}
	}
	bucket.RUnlock()

	// Find our neighbors at the desired precision
	broadcastNeighbors := source.Neighbors(precision)

	for _, neighbor := range broadcastNeighbors {
		// Iterate over each address in the bucket to
		// see if it overlaps with the desired precision.
		neighborGeohash := NewGeohashManual(neighbor, precision)
		bucket = p.world.Bucket(neighborGeohash.AsPrecision(geohashBucketPrecisionBits))
		for _, addr := range bucket.RLock() {
			if addr == self {
				continue
			}
			if addr.Geohash().AsPrecision(precision) == neighborGeohash.AsPrecision(precision) {
				addr.SendMail(mail.Packet())
			}
		}
		bucket.RUnlock()
	}
}

// removeAddressFromGeohashBucket removes an address from the geohash bucket `bucketGeohash`.
// `bucketGeohash` must be encoded with geohashBucketPrecisionBits bits.
func (p *PostOffice) removeAddressFromGeohashBucket(bucketGeohash uint64, address Address) {
	p.world.Bucket(bucketGeohash).Delete(address)
}

// addAddressToGeohashBucket adds an address to the geohash bucket `bucketGeohash`.
// `bucketGeohash` must be encoded with geohashBucketPrecisionBits bits.
func (p *PostOffice) addAddressToGeohashBucket(bucketGeohash uint64, address Address) {
	p.world.Bucket(bucketGeohash).Add(address)
}
