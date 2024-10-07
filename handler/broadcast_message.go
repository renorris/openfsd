package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
)

func broadcastMessageHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.TextMessagePDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Ignore if the user doesn't have permission
	if invoker.NetworkRating() < protocol.NetworkRatingSUP {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, "", "insufficient permission to broadcast")
		return
	}

	// Verify the To field is set to `*`
	if pdu.To != "*" {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, pdu.To, "broadcast message recipient must be '*'")
		return
	}

	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeBroadcast, "", packet)
	result.addMail(mail)

	return
}
