package handler

import (
	"github.com/renorris/openfsd/protocol"
	"strconv"
)

func deletePilotHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.DeletePilotPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Check for invalid CID
	if pdu.CID != invoker.CID() {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, strconv.Itoa(pdu.CID), "incorrect CID")
		return
	}

	result.addReply((&protocol.TextMessagePDU{
		From:    protocol.ServerCallsign,
		To:      pdu.From,
		Message: "Goodbye!",
	}).Serialize())

	result.setDisconnectFlag()

	return
}
