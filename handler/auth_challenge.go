package handler

import (
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/protocol/vatsimauth"
)

func authChallengeHandler(invoker Invoker, packet string) (result Result, err error) {
	// Parse packet
	pdu := protocol.AuthChallengePDU{}
	if err = pdu.Parse(packet); err != nil {
		return
	}

	// Verify source callsign
	if pdu.From != invoker.Callsign() {
		return pduSourceInvalidResult()
	}

	// Generate response
	challengeResponse := invoker.AuthSelf().GenerateResponse(pdu.Challenge)
	invoker.AuthSelf().UpdateState(challengeResponse)

	challengeResponsePDU := protocol.AuthChallengeResponsePDU{
		From:              protocol.ServerCallsign,
		To:                invoker.Callsign(),
		ChallengeResponse: challengeResponse,
	}

	// Send a counter-challenge
	var chal string
	if chal, err = vatsimauth.GenerateChallenge(); err != nil {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, "", "internal server error: error generating counter-challenge string")
		return
	}

	invoker.SetPendingChallenge(chal)

	newChallengePDU := protocol.AuthChallengePDU{
		From:      protocol.ServerCallsign,
		To:        invoker.Callsign(),
		Challenge: invoker.PendingChallenge(),
	}

	result.addReply(challengeResponsePDU.Serialize())
	result.addReply(newChallengePDU.Serialize())

	return
}
