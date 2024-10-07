package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
)

func fastPilotPositionHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.FastPilotPositionPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Update location for post office if slow/stopped type
	switch pdu.Type {
	case protocol.FastPilotPositionTypeSlow, protocol.FastPilotPositionTypeStopped:
		invoker.SetGeohash(servercontext.PostOffice().SetLocation(invoker.Address(), pdu.Lat, pdu.Lng))
	}

	// Proximity broadcast position update
	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeCloseProximityBroadcast, "", packet)
	result.addMail(mail)

	return
}
