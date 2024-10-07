package handler

import (
	"github.com/renorris/openfsd/protocol"
)

func authChallengeResponseHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.AuthChallengeResponsePDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Verify the response with the stored pending challenge
	var res string
	if res = invoker.AuthVerify().GenerateResponse(invoker.PendingChallenge()); res != pdu.ChallengeResponse {
		result.setDisconnectFlag()
		err = protocol.NewGenericFSDError(protocol.UnauthorizedSoftwareError, pdu.ChallengeResponse, "incorrect challenge response")
		return
	}

	invoker.AuthVerify().UpdateState(res)

	return
}
