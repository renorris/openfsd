package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"strings"
)

func clientQueryHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.ClientQueryPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	switch pdu.To {
	case protocol.ServerCallsign:
		// Only handle IP queries
		if pdu.QueryType != protocol.ClientQueryPublicIP {
			return
		}

		ip := strings.Split(invoker.RemoteNetworkAddrString(), ":")[0]
		responsePDU := protocol.ClientQueryResponsePDU{
			From:      protocol.ServerCallsign,
			To:        invoker.Callsign(),
			QueryType: protocol.ClientQueryPublicIP,
			Payload:   ip,
		}
		result.addReply(responsePDU.Serialize())
		return

	case protocol.ClientQueryBroadcastRecipient, protocol.ClientQueryBroadcastRecipientPilots:
		// Proximity broadcast
		mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeDirect, pdu.To, packet)
		result.addMail(mail)
		return
	}

	// Assume direct message client query
	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeDirect, pdu.To, packet)
	result.addMail(mail)

	return
}
