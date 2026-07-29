// Package adapters registers known clientinject.Adapter implementations.
package adapters

import (
	"github.com/renorris/openfsd/internal/clientinject"
	"github.com/renorris/openfsd/internal/clientinject/adapters/vpilot"
	"github.com/renorris/openfsd/internal/clientinject/adapters/xpilot"
)

// DefaultAdapters returns the built-in client adapters (vPilot first, then xPilot).
func DefaultAdapters() []clientinject.Adapter {
	return []clientinject.Adapter{
		vpilot.New(),
		xpilot.New(),
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
