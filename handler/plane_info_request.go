package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
)

func planeInfoRequestHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.PlaneInfoRequestPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Forward to recipient
	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeDirect, pdu.To, packet)
	result.addMail(mail)

	return
}
