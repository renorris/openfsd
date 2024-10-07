package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
)

func wallopMessageHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.TextMessagePDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Verify the To field is set to `*S`
	if pdu.To != "*S" {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, pdu.To, "wallop recipient must be *S")
		return
	}

	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeSupervisorBroadcast, "", packet)
	result.addMail(mail)

	return
}
