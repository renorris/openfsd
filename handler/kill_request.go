package handler

import (
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"strconv"
)

func killRequestHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.KillRequestPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Check if user has permission
	if invoker.NetworkRating() < protocol.NetworkRatingSUP {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, strconv.Itoa(int(invoker.NetworkRating())), "insufficient permission to kill")
		return
	}

	replyPDU := protocol.TextMessagePDU{
		From:    protocol.ServerCallsign,
		To:      invoker.Callsign(),
		Message: "",
	}

	if err = servercontext.PostOffice().Kill(&pdu); err != nil {
		replyPDU.Message = err.Error()
		result.addReply(replyPDU.Serialize())
		return result, nil
	}

	replyPDU.Message = "killed " + pdu.To

	result.addReply(replyPDU.Serialize())

	return
}
