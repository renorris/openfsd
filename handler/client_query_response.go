package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
)

func clientQueryResponseHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.ClientQueryResponsePDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Ignore responses to server callsign
	if pdu.To == protocol.ServerCallsign {
		return
	}

	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeDirect, pdu.To, packet)
	result.addMail(mail)

	return
}
