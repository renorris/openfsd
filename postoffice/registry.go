package postoffice

import (
	"errors"
	"sync"
)

// Registry is a concurrency-safe map.
// It does not allow a key to be overwritten if it already exists.
type Registry struct {
	lock     sync.RWMutex
	registry map[string]Address
}

// NewRegistry makes a new registry with `alloc` spaces pre-allocated.
func NewRegistry(alloc int) *Registry {
	return &Registry{
		lock:     sync.RWMutex{},
		registry: make(map[string]Address, alloc),
	}
}

func keyInUseError() error {
	return errors.New("key in use")
}

var KeyInUseError = keyInUseError()

// Store adds a key/value pair into the registry.
// Returns KeyInUseError if the key is already in use.
func (r *Registry) Store(key string, val Address) error {
	r.lock.Lock()

	_, exists := r.registry[key]
	if exists {
		r.lock.Unlock()
		return KeyInUseError
	}
	r.registry[key] = val

	r.lock.Unlock()
	return nil
}

// Delete removes a key/value pair from the registry.
func (r *Registry) Delete(key string) {
	r.lock.Lock()

	delete(r.registry, key)

	r.lock.Unlock()
}

// Load fetches a value for a given key
func (r *Registry) Load(key string) (addr Address, exists bool) {
	r.lock.RLock()

	addr, exists = r.registry[key]

	r.lock.RUnlock()
	return
}

// Len returns the number of keys stored in this registry
func (r *Registry) Len() int {
	r.lock.RLock()

	length := len(r.registry)

	r.lock.RUnlock()
	return length
}

// ForEach calls the provided function once for each key in this registry.
// If the function returns false, iteration will cease and return early.
func (r *Registry) ForEach(f func(key string, val Address) bool) {
	r.lock.RLock()

	for k, v := range r.registry {
		if !f(k, v) {
			return
		}
	}

	r.lock.RUnlock()
}
