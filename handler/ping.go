package handler

import (
	"github.com/renorris/openfsd/protocol"
)

func pingHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.PingPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Ignore if the ping isn't for the server
	if pdu.To != protocol.ServerCallsign {
		return
	}

	pongPDU := protocol.PongPDU{
		From:      protocol.ServerCallsign,
		To:        invoker.Callsign(),
		Timestamp: pdu.Timestamp,
	}
	result.addReply(pongPDU.Serialize())

	return
}
