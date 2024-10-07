package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/protocol/vatsimauth"
)

// Invoker represents an invoker of a handler function
type Invoker interface {
	Callsign() string

	AuthSelf() *vatsimauth.VatsimAuth
	AuthVerify() *vatsimauth.VatsimAuth
	PendingChallenge() string
	SetPendingChallenge(string)

	NetworkRating() protocol.NetworkRating
	CID() int
	SetGeohash(postoffice.Geohash)
	SendFastEnabled() bool
	SetSendFastEnabled(bool)

	// Address returns the Address representation of this invoker
	Address() postoffice.Address
	SetAddressState(*postoffice.AddressState)

	// RemoteNetworkAddrString returns the remote TCP address associated with this invoker's underlying network connection
	RemoteNetworkAddrString() string
}
