package postoffice

import (
	"errors"
	"github.com/renorris/openfsd/protocol"
	"time"
)

// Address represents a routable address for a PostOffice
type Address interface {
	Name() string
	SendMail(string)
	SendKill(string) error
	NetworkRating() protocol.NetworkRating
	Geohash() Geohash
	State() AddressState
}

// AddressState represents address metadata
type AddressState struct {
	CID         int
	RealName    string
	PilotRating protocol.PilotRating
	Latitude    float64
	Longitude   float64
	Altitude    int
	Groundspeed int
	Transponder string
	Heading     int       // Degrees magnetic
	LastUpdated time.Time // The time this pilot's information was last updated
}

func addressAlreadyRegisteredError() error { return errors.New("address name in use") }
func addressNotRegisteredError() error     { return errors.New("address not registered") }

var AddressAlreadyRegisteredError = addressAlreadyRegisteredError()
var AddressNotRegisteredError = addressNotRegisteredError()
