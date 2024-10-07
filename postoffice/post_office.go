package postoffice

import (
	"github.com/renorris/openfsd/protocol"
)

// Addresses are placed into geographical buckets.
// A bucket is determined by lowering the precision of an address
// into a 15-bit geohash "bounding box."
// For proximity broadcasts, the caller address's source bucket is determined,
// then each neighbor is sent the message. This does not magically avoid the
// O(n^2) problem, but it shrinks it to the point of disregard in 99.9% of
// reasonable cases.

// PostOffice handles the routing of messages between clients
type PostOffice struct {
	addressRegistry           *Registry // Map address name (usually a callsign) to its respective Address
	supervisorAddressRegistry *Registry // Map supervisor address name to its respective Address
	world                     *World    // World container holding all geohash buckets
}

func NewPostOffice() *PostOffice {
	return &PostOffice{
		addressRegistry:           NewRegistry(1024),
		supervisorAddressRegistry: NewRegistry(16),
		world:                     NewWorld(),
	}
}

// RegisterAddress registers an address to the post
// office, making it a valid recipient for other addresses.
// Returns KeyInUseError if the address name (callsign) is already in use.
func (p *PostOffice) RegisterAddress(address Address) error {
	if err := p.addressRegistry.Store(address.Name(), address); err != nil {
		return err
	}

	// Always acquire a key in the main address registry first *before* adding anything to the supervisor map.
	// On delete, perform the operations in the reverse order: remove from supervisor registry,
	// then finally remove from main address registry.

	// If the address is a supervisor, add them to the supervisor registry
	if address.NetworkRating() >= protocol.NetworkRatingSUP {
		if err := p.supervisorAddressRegistry.Store(address.Name(), address); err != nil {
			return err
		}
	}

	return nil
}

// DeregisterAddress removes an address from the post office.
func (p *PostOffice) DeregisterAddress(address Address) {
	// Remove any supervisor entry *first.*
	if address.NetworkRating() >= protocol.NetworkRatingSUP {
		p.supervisorAddressRegistry.Delete(address.Name())
	}

	p.addressRegistry.Delete(address.Name())

	p.removeAddressFromGeohashBucket(address.Geohash().AsPrecision(geohashBucketPrecisionBits), address)
}

// NumRegistered returns a snapshot of how many addresses were registered at the time of calling
func (p *PostOffice) NumRegistered() int {
	return p.addressRegistry.Len()
}

// ForEachRegistered runs the provided function for each registered address.
// If the function returns false, iteration will cease and return early.
func (p *PostOffice) ForEachRegistered(f func(name string, Address Address) bool) {
	p.addressRegistry.ForEach(f)
}

// SendMail forwards Mail to its recipients
func (p *PostOffice) SendMail(mail *Mail) {
	switch mail.Type() {
	case MailTypeDirect:
		p.sendDirectMail(mail)
	case MailTypeGeneralProximityBroadcast:
		p.sendProximityMail(mail, geohashGeneralProximityPrecisionBits)
	case MailTypeCloseProximityBroadcast:
		p.sendProximityMail(mail, geohashCloseProximityPrecisionBits)
	case MailTypeBroadcast:
		p.sendBroadcastMail(mail)
	case MailTypeSupervisorBroadcast:
		p.sendSupervisorBroadcastMail(mail)
	}
}

// SetLocation marks where an address is geographically located in order to
// properly handle any future proximity broadcast messages.
// Returns the updated geohash.
func (p *PostOffice) SetLocation(address Address, lat, lng float64) (newGeohash Geohash) {

	// Make a full precision geohash from the caller's coordinates
	newGeohash = NewGeohash(lat, lng)

	// Check if the provided coordinates lie outside the address's current geohash
	currentBucketHash := address.Geohash().AsPrecision(geohashBucketPrecisionBits)
	newBucketHash := newGeohash.AsPrecision(geohashBucketPrecisionBits)
	if currentBucketHash == newBucketHash {
		return
	}

	// Remove the address from the current bucket
	p.removeAddressFromGeohashBucket(currentBucketHash, address)

	// Put it into the new bucket
	p.addAddressToGeohashBucket(newBucketHash, address)

	return
}

// Kill finds an address associated with `pdu` and sends a kill signal
func (p *PostOffice) Kill(pdu *protocol.KillRequestPDU) error {
	addr, exists := p.addressRegistry.Load(pdu.To)
	if !exists {
		return AddressNotRegisteredError
	}

	return addr.SendKill(pdu.Serialize())
}
