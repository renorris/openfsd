package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"strings"
)

func radioMessageHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.TextMessagePDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Verify the To field is a radio frequency
	if !strings.HasPrefix(pdu.To, "@") || len(pdu.To) != 6 {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, pdu.To, "recipient must be a radio frequency @XXXXX e.g. @22800")
		return
	}

	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeGeneralProximityBroadcast, "", packet)
	result.addMail(mail)

	return
}
