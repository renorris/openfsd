package main

import (
	"errors"
	"github.com/renorris/openfsd/protocol"
	"strings"
)

// ProcessorResult represents the result of a handler function
type ProcessorResult struct {
	Replies          []string
	Mail             []Mail
	ShouldDisconnect bool
}

func NewProcessorResult() *ProcessorResult {
	return &ProcessorResult{
		Replies:          nil,
		Mail:             nil,
		ShouldDisconnect: false,
	}
}

// Disconnect sets the flag to disconnect the client
func (r *ProcessorResult) Disconnect(flag bool) {
	r.ShouldDisconnect = flag
}

// AddReply adds a packet to send back to the client
func (r *ProcessorResult) AddReply(packet string) {
	if r.Replies == nil {
		r.Replies = make([]string, 0)
	}
	r.Replies = append(r.Replies, packet)
}

// AddMail adds mail to be sent to other clients
func (r *ProcessorResult) AddMail(mail Mail) {
	if r.Mail == nil {
		r.Mail = make([]Mail, 0)
	}
	r.Mail = append(r.Mail, mail)
}

// Processor represents a function to process an incoming FSD packet
type Processor func(client *FSDClient, rawPacket string) *ProcessorResult

// InvalidPacketError means the packet was not recognized by the parser
func newInvalidPacketError() error {
	return errors.New("Invalid packet")
}

var (
	InvalidPacketError = newInvalidPacketError()
)

func GetProcessor(rawPacket string) (Processor, error) {
	rawPacket = strings.TrimSuffix(rawPacket, "\r\n")
	if len(rawPacket) < 3 {
		return nil, InvalidPacketError
	}

	fields := strings.Split(rawPacket, protocol.Delimeter)

	switch rawPacket[0] {
	case '^':
		return FastPilotPositionProcessor, nil
	case '@':
		return PilotPositionProcessor, nil
	case '#', '$':
		pduID := rawPacket[0:3]
		switch pduID {
		case "$CQ":
			return ClientQueryProcessor, nil
		case "$CR":
			return ClientQueryResponseProcessor, nil
		case "#SL":
			return FastPilotPositionProcessor, nil
		case "#ST":
			return FastPilotPositionProcessor, nil
		case "$AX":
			// TODO: implement METAR request
		case "$PI":
			return PingProcessor, nil
		case "$ZC":
			return AuthChallengeProcessor, nil
		case "$ZR":
			return AuthChallengeResponseProcessor, nil
		case "$!!":
			return KillRequestProcessor, nil
		case "#DP":
			return DeletePilotProcessor, nil
		case "#SB":
			if len(fields) < 3 {
				return nil, InvalidPacketError
			}
			if fields[2] == "PIR" {
				return PlaneInfoRequestProcessor, nil
			}
			if fields[2] == "PI" && len(fields) > 3 && fields[3] == "GEN" {
				return PlaneInfoResponseProcessor, nil
			}
		case "#TM":
			if len(fields) > 3 {
				return nil, InvalidPacketError
			}
			switch fields[1] {
			case "*":
				return BroadcastMessageProcessor, nil
			case "*S":
				return WallopMessageProcessor, nil
			default:
				if len(fields[1]) > 0 && fields[1][0:1] == "@" {
					return RadioMessageProcessor, nil
				} else {
					return DirectMessageProcessor, nil
				}
			}
		}
	}

	return nil, InvalidPacketError
}
