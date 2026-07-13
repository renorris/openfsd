package server

import (
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// PacketType re-exports protocol.PacketType so server handlers keep compiling.
type PacketType = protocol.PacketType

const (
	PacketTypeUnknown              = protocol.PacketTypeUnknown
	PacketTypeTextMessage          = protocol.PacketTypeTextMessage
	PacketTypePilotPosition        = protocol.PacketTypePilotPosition
	PacketTypePilotPositionFast    = protocol.PacketTypePilotPositionFast
	PacketTypePilotPositionSlow    = protocol.PacketTypePilotPositionSlow
	PacketTypePilotPositionStopped = protocol.PacketTypePilotPositionStopped
	PacketTypeATCPosition          = protocol.PacketTypeATCPosition
	PacketTypeDeleteATC            = protocol.PacketTypeDeleteATC
	PacketTypeDeletePilot          = protocol.PacketTypeDeletePilot
	PacketTypeClientQuery          = protocol.PacketTypeClientQuery
	PacketTypeClientQueryResponse  = protocol.PacketTypeClientQueryResponse
	PacketTypeProController        = protocol.PacketTypeProController
	PacketTypeSquawkbox            = protocol.PacketTypeSquawkbox
	PacketTypeMetarRequest         = protocol.PacketTypeMetarRequest
	PacketTypeKillRequest          = protocol.PacketTypeKillRequest
	PacketTypeAuthChallenge        = protocol.PacketTypeAuthChallenge
	PacketTypeHandoffRequest       = protocol.PacketTypeHandoffRequest
	PacketTypeHandoffAccept        = protocol.PacketTypeHandoffAccept
	PacketTypeFlightPlan           = protocol.PacketTypeFlightPlan
	PacketTypeFlightPlanAmendment  = protocol.PacketTypeFlightPlanAmendment
)

// getPacketType parses the packet type given a packet.
// Login/error packet types known to protocol.TypeOf are mapped back to
// PacketTypeUnknown so post-login verifyPacket behavior is unchanged.
func getPacketType(packet []byte) PacketType {
	t := protocol.TypeOf(packet)
	switch t {
	case protocol.PacketTypeServerIdent,
		protocol.PacketTypeClientIdent,
		protocol.PacketTypeAddPilot,
		protocol.PacketTypeAddATC,
		protocol.PacketTypeError:
		return PacketTypeUnknown
	default:
		return t
	}
}

func getPacketPrefix(packetType PacketType) string {
	return protocol.Prefix(packetType)
}

func minFields(packetType PacketType) int {
	return protocol.MinFields(packetType)
}

type handlerFunc func(client *session.Session, packet []byte)

func getSourceCallsign(packet []byte, packetType PacketType) []byte {
	return protocol.SourceCallsign(packet, packetType)
}

func verifySourceCallsign(packet []byte, packetType PacketType, callsign string) bool {
	return protocol.VerifySourceCallsign(packet, packetType, callsign)
}

// verifyPacket runs a set of sanity checks against a packet sent by a client and returns the detected packet type
func verifyPacket(packet []byte, client *session.Session) (packetType PacketType, ok bool) {
	numFields := countFields(packet)
	if len(packet) < 8 || numFields < 3 {
		client.SendError(SyntaxError, "Packet too short")
		return
	}

	packetType = getPacketType(packet)
	if packetType == PacketTypeUnknown {
		client.SendError(SyntaxError, "Unknown packet type")
		return
	}

	if !verifySourceCallsign(packet, packetType, client.Callsign) {
		client.SendError(SourceInvalidError, "Source invalid")
		return
	}

	if numFields < minFields(packetType) {
		client.SendError(SyntaxError, "Minimum field count requirement not satisfied")
		return
	}

	ok = true
	return
}
