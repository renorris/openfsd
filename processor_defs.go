package main

import (
	"errors"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/vatsimauth"
	"log"
	"strings"
)

func PilotPositionProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParsePilotPositionPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Update location for post office
	PO.SetLocation(client, pdu.Lat, pdu.Lng)

	result := NewProcessorResult()

	// Check if we should update SendFastEnabled
	if client.SendFastEnabled && pdu.GroundSpeed == 0 {
		client.SendFastEnabled = false
		disableSendFastPDU := protocol.SendFastPDU{
			From:       protocol.ServerCallsign,
			To:         client.Callsign,
			DoSendFast: false,
		}
		result.AddReply(disableSendFastPDU.Serialize())
	} else if !client.SendFastEnabled && pdu.GroundSpeed > 0 {
		client.SendFastEnabled = true
		enableSendFastPDU := protocol.SendFastPDU{
			From:       protocol.ServerCallsign,
			To:         client.Callsign,
			DoSendFast: true,
		}
		result.AddReply(enableSendFastPDU.Serialize())
	}

	// Broadcast position update
	mail := NewMail(client)
	mail.SetType(MailTypeBroadcastRanged)
	mail.AddPacket(rawPacket)

	result.AddMail(*mail)
	return result
}

func ClientQueryProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseClientQueryPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	switch pdu.To {
	case protocol.ServerCallsign:
		if pdu.QueryType == protocol.ClientQueryPublicIP {
			ip := strings.Split(client.Conn.RemoteAddr().String(), ":")[0]
			responsePDU := protocol.ClientQueryResponsePDU{
				From:      protocol.ServerCallsign,
				To:        client.Callsign,
				QueryType: protocol.ClientQueryPublicIP,
				Payload:   ip,
			}
			result := NewProcessorResult()
			result.AddReply(responsePDU.Serialize())
			return result
		}

		return NewProcessorResult()

	case protocol.ClientQueryBroadcastRecipient, protocol.ClientQueryBroadcastRecipientPilots:
		mail := NewMail(client)
		mail.SetType(MailTypeBroadcastRanged)
		mail.AddRecipient(pdu.To)
		mail.AddPacket(rawPacket)

		result := NewProcessorResult()
		result.AddMail(*mail)

		return result
	}

	// Assume direct message client query
	mail := NewMail(client)
	mail.SetType(MailTypeDirect)
	mail.AddRecipient(pdu.To)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result

}

func ClientQueryResponseProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseClientQueryResponsePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	if pdu.To == protocol.ServerCallsign {
		return NewProcessorResult()
	}

	mail := NewMail(client)
	mail.SetType(MailTypeDirect)
	mail.AddRecipient(pdu.To)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func PlaneInfoRequestProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParsePlaneInfoRequestPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	mail := NewMail(client)
	mail.SetType(MailTypeDirect)
	mail.AddRecipient(pdu.To)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func PlaneInfoResponseProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParsePlaneInfoResponsePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	mail := NewMail(client)
	mail.SetType(MailTypeDirect)
	mail.AddRecipient(pdu.To)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func FastPilotPositionProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseFastPilotPositionPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Update location for post office if slow/stopped type
	switch pdu.Type {
	case protocol.FastPilotPositionTypeSlow, protocol.FastPilotPositionTypeStopped:
		PO.SetLocation(client, pdu.Lat, pdu.Lng)
	}

	// Broadcast position update
	mail := NewMail(client)
	mail.SetType(MailTypeBroadcastRanged)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)
	return result
}

func AuthChallengeProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseAuthChallengePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Generate response for the client's challenge
	challengeResponse := client.AuthSelf.GenerateResponse(pdu.Challenge)
	client.AuthSelf.UpdateState(challengeResponse)
	challengeResponsePDU := protocol.AuthChallengeResponsePDU{
		From:              protocol.ServerCallsign,
		To:                client.Callsign,
		ChallengeResponse: challengeResponse,
	}

	// Send the client a new auth challenge
	client.PendingAuthVerifyChallenge, err = vatsimauth.GenerateChallenge()
	if err != nil {
		log.Println("Error generating challenge string")
		result := NewProcessorResult()
		result.Disconnect(true)
		return result
	}

	newChallengePDU := protocol.AuthChallengePDU{
		From:      protocol.ServerCallsign,
		To:        client.Callsign,
		Challenge: client.PendingAuthVerifyChallenge,
	}

	result := NewProcessorResult()
	result.AddReply(challengeResponsePDU.Serialize())
	result.AddReply(newChallengePDU.Serialize())

	return result
}

func AuthChallengeResponseProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseAuthChallengeResponsePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Verify the response with the stored pending challenge
	challengeResponse := client.AuthVerify.GenerateResponse(client.PendingAuthVerifyChallenge)
	if challengeResponse != pdu.ChallengeResponse {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.UnauthorizedSoftwareError).Serialize())
		result.Disconnect(true)
		return result
	}
	client.AuthVerify.UpdateState(challengeResponse)

	result := NewProcessorResult()
	return result
}

func DeletePilotProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseDeletePilotPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Check for valid CID
	if pdu.CID != client.CID {
		result := NewProcessorResult()

		result.AddReply(protocol.NewGenericFSDError(protocol.SyntaxError).Serialize())
		result.Disconnect(true)

		return result
	}

	result := NewProcessorResult()
	result.Disconnect(true)

	return result
}

func PingProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParsePingPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Ignore if the ping isn't for the server
	if pdu.To != protocol.ServerCallsign {
		result := NewProcessorResult()
		return result
	}

	result := NewProcessorResult()
	pongPDU := protocol.PongPDU{
		From:      protocol.ServerCallsign,
		To:        client.Callsign,
		Timestamp: pdu.Timestamp,
	}
	result.AddReply(pongPDU.Serialize())

	return result
}

func BroadcastMessageProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseTextMessagePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Ignore if the user doesn't have permission
	if client.NetworkRating < protocol.NetworkRatingSUP {
		return NewProcessorResult()
	}

	// Verify the To field is set to `*`
	if pdu.To != "*" {
		return NewProcessorResult()
	}

	mail := NewMail(client)
	mail.SetType(MailTypeBroadcastAll)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func WallopMessageProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseTextMessagePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Verify the To field is set to `*S`
	if pdu.To != "*S" {
		return NewProcessorResult()
	}

	mail := NewMail(client)
	mail.SetType(MailTypeBroadcastSupervisors)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func RadioMessageProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseTextMessagePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Verify the To field is a radio frequency
	if !strings.HasPrefix(pdu.To, "@") || len(pdu.To) != 6 {
		return NewProcessorResult()
	}

	mail := NewMail(client)
	mail.SetType(MailTypeBroadcastRanged)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func DirectMessageProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseTextMessagePDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	mail := NewMail(client)
	mail.SetType(MailTypeDirect)
	mail.AddRecipient(pdu.To)
	mail.AddPacket(rawPacket)

	result := NewProcessorResult()
	result.AddMail(*mail)

	return result
}

func KillRequestProcessor(client *FSDClient, rawPacket string) *ProcessorResult {
	// Parse & validate packet
	pdu, err := protocol.ParseKillRequestPDU(rawPacket)
	if err != nil {
		var fsdError *protocol.FSDError
		result := NewProcessorResult()
		if errors.As(err, &fsdError) {
			result.AddReply(fsdError.Serialize())
		}
		result.Disconnect(true)
		return result
	}

	// Check for valid source callsign
	if pdu.From != client.Callsign {
		result := NewProcessorResult()
		result.AddReply(protocol.NewGenericFSDError(protocol.PDUSourceInvalidError).Serialize())
		result.Disconnect(true)
		return result
	}

	// Check if client has permission
	if client.NetworkRating < protocol.NetworkRatingSUP {
		return NewProcessorResult()
	}

	victim, err := PO.GetClient(pdu.To)
	if err != nil {
		result := NewProcessorResult()
		if errors.Is(err, CallsignNotRegisteredError) {
			result.AddReply(protocol.NewGenericFSDError(protocol.NoSuchCallsignError).Serialize())
		}
		return result
	}

	result := NewProcessorResult()
	replyPDU := protocol.TextMessagePDU{
		From:    protocol.ServerCallsign,
		To:      client.Callsign,
		Message: "",
	}

	// Attempt a non-blocking kill
	select {
	case victim.Kill <- rawPacket:
		replyPDU.Message = "Killed " + pdu.To
	default:
		replyPDU.Message = "ERROR: unable to kill " + pdu.To
	}

	result.AddReply(replyPDU.Serialize())
	return result
}
