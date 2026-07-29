// Package adapters registers known clientinject.Adapter implementations.
package adapters

import (
	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/adapters/vpilot"
)

// DefaultAdapters returns the built-in client adapters.
// Today: Windows vPilot only.
func DefaultAdapters() []clientinject.Adapter {
	return []clientinject.Adapter{
		vpilot.New(),
	}
}

// DefaultEngine loads embedded profiles and registers DefaultAdapters.
func DefaultEngine() (*clientinject.Engine, error) {
	store, err := clientinject.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	return clientinject.NewEngine(store, DefaultAdapters()...), nil
}
