package handler

import (
	"github.com/renorris/openfsd/protocol"
	"strings"
)

// Handler represents a function to process an FSD packet
type Handler func(invoker Invoker, packet string) (Result, error)

// New parses the provided packet's identifier and returns a handler function
func New(packet string) (handler Handler, err error) {
	// Trim packet delimiter and verify length
	if packet = strings.TrimSuffix(packet, protocol.PacketDelimiter); len(packet) < 3 {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, "", "invalid packet: too short")
		return
	}

	fields := strings.Split(packet, protocol.Delimiter)

	switch packet[0] {
	case '^':
		handler = fastPilotPositionHandler
	case '@':
		handler = pilotPositionHandler
	case '$':
		pduID := packet[0:3]
		switch pduID {
		case "$CQ":
			handler = clientQueryHandler
		case "$CR":
			handler = clientQueryResponseHandler
		case "$ZC":
			handler = authChallengeHandler
		case "$ZR":
			handler = authChallengeResponseHandler
		case "$!!":
			handler = killRequestHandler
		case "$PI":
			handler = pingHandler
		case "$AX":
			// TODO: implement METAR request
		}
	case '#':
		pduID := packet[0:3]
		switch pduID {
		case "#SL":
			handler = fastPilotPositionHandler
		case "#ST":
			handler = fastPilotPositionHandler
		case "#DP":
			handler = deletePilotHandler
		case "#SB":
			if len(fields) < 3 {
				err = protocol.NewGenericFSDError(protocol.SyntaxError, "", "unrecognized #SB packet: invalid parameter count")
				return
			}
			switch fields[2] {
			case "PIR":
				handler = planeInfoRequestHandler
			case "FSIPIR":
				handler = planeInfoRequestFsinnHandler
			case "PI":
				if len(fields) > 3 && fields[3] == "GEN" {
					handler = planeInfoResponseHandler
				}
			}
		case "#TM":
			if len(fields) < 3 {
				err = protocol.NewGenericFSDError(protocol.SyntaxError, "", "unrecognized #TM packet: invalid parameter count")
				return
			}
			switch fields[1] {
			case "*":
				handler = broadcastMessageHandler
			case "*S":
				handler = wallopMessageHandler
			default:
				if len(fields[1]) > 0 && fields[1][0] == '@' {
					handler = radioMessageHandler
				} else {
					handler = directMessageHandler
				}
			}
		}
	}

	if handler == nil {
		err = protocol.NewGenericFSDError(protocol.SyntaxError, "", "unrecognized packet type")
		return
	}

	return
}

func pduSourceInvalidResult() (result Result, err error) {
	err = protocol.NewGenericFSDError(protocol.PDUSourceInvalidError, "", "")
	return
}
