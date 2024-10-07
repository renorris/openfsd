package postoffice

import (
	"slices"
	"sync"
)

// GeohashBucket is a contiguous & concurrency-safe array of addresses
type GeohashBucket struct {
	lock      sync.RWMutex
	addresses []Address
}

// RLock locks this bucket for read-only access, then returns the slice of addresses to read.
// Conventionally, callers must use RUnlock() once done reading addresses.
func (e *GeohashBucket) RLock() (addresses []Address) {
	e.lock.RLock()
	return e.addresses
}

// RUnlock releases the read lock for this entry
func (e *GeohashBucket) RUnlock() {
	e.lock.RUnlock()
}

// Delete deletes an address from this bucket's list.
// Returns the Len of the underlying list after deleting the value.
func (e *GeohashBucket) Delete(address Address) {
	e.lock.Lock()

	// Find index of entry to remove
	i := slices.Index(e.addresses, address)
	if i > -1 {
		// Move last element into element we're deleting
		e.addresses[i] = e.addresses[len(e.addresses)-1]

		// Change slice header to reflect new Len
		e.addresses = e.addresses[:len(e.addresses)-1]

		// Reallocate the slice if the capacity is twice the Len
		if len(e.addresses) > 0 && cap(e.addresses)/len(e.addresses) > 1 {
			newSlice := make([]Address, len(e.addresses))
			copy(newSlice, e.addresses)
			e.addresses = newSlice
		}
	}

	e.lock.Unlock()
}

// Add adds an address to this bucket's list
func (e *GeohashBucket) Add(address Address) {
	e.lock.Lock()
	e.addresses = append(e.addresses, address)
	e.lock.Unlock()
}
