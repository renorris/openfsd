package handler

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"time"
)

func pilotPositionHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.PilotPositionPDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Write location to post office
	invoker.SetGeohash(servercontext.PostOffice().SetLocation(invoker.Address(), pdu.Lat, pdu.Lng))

	// Update SendFastEnabled state if required
	if invoker.SendFastEnabled() && pdu.GroundSpeed == 0 {
		invoker.SetSendFastEnabled(false)
		disableSendFastPDU := protocol.SendFastPDU{
			From:       protocol.ServerCallsign,
			To:         invoker.Callsign(),
			DoSendFast: false,
		}
		result.addReply(disableSendFastPDU.Serialize())
	} else if !invoker.SendFastEnabled() && pdu.GroundSpeed > 0 {
		invoker.SetSendFastEnabled(true)
		enableSendFastPDU := protocol.SendFastPDU{
			From:       protocol.ServerCallsign,
			To:         invoker.Callsign(),
			DoSendFast: true,
		}
		result.addReply(enableSendFastPDU.Serialize())
	}

	// Update invoker address state
	state := invoker.Address().State()

	state.Latitude = pdu.Lat
	state.Longitude = pdu.Lng
	state.Heading = int(pdu.Heading)
	state.Groundspeed = pdu.GroundSpeed
	state.Altitude = pdu.TrueAltitude
	state.Transponder = pdu.SquawkCode
	state.LastUpdated = time.Now()

	invoker.SetAddressState(&state)

	// Proximity broadcast position update
	mail := postoffice.NewMail(invoker.Address(), postoffice.MailTypeGeneralProximityBroadcast, "", packet)
	result.addMail(mail)

	return
}
