package fsd

import "bytes"

type PacketType int

const (
	PacketTypeUnknown PacketType = iota
	PacketTypeTextMessage
	PacketTypePilotPosition
	PacketTypePilotPositionFast
	PacketTypePilotPositionSlow
	PacketTypePilotPositionStopped
	PacketTypeATCPosition
	PacketTypeDeleteATC
	PacketTypeDeletePilot
	PacketTypeClientQuery
	PacketTypeClientQueryResponse
	PacketTypeProController
	PacketTypeSquawkbox
	PacketTypeMetarRequest
	PacketTypeKillRequest
	PacketTypeAuthChallenge
	PacketTypeHandoffRequest
	PacketTypeHandoffAccept
	PacketTypeFlightPlan
	PacketTypeFlightPlanAmendment
)

// sourceCallsignFieldIndex returns the index of the field containing the source callsign
func sourceCallsignFieldIndex(packetType PacketType) (index int) {
	switch packetType {
	case PacketTypePilotPosition:
		return 1
	default:
		return 0
	}
}

// getPacketType parses the packet type given a packet
func getPacketType(packet []byte) PacketType {
	switch packet[0] {
	case '^':
		return PacketTypePilotPositionFast
	case '@':
		return PacketTypePilotPosition
	case '%':
		return PacketTypeATCPosition
	case '#':
		switch string(packet[:3]) {
		case "#DA":
			return PacketTypeDeleteATC
		case "#DP":
			return PacketTypeDeletePilot
		case "#TM":
			return PacketTypeTextMessage
		case "#SL":
			return PacketTypePilotPositionSlow
		case "#ST":
			return PacketTypePilotPositionStopped
		case "#PC":
			return PacketTypeProController
		case "#SB":
			return PacketTypeSquawkbox
		default:
			return PacketTypeUnknown
		}
	case '$':
		switch string(packet[:3]) {
		case "$CQ":
			return PacketTypeClientQuery
		case "$CR":
			return PacketTypeClientQueryResponse
		case "$AX":
			return PacketTypeMetarRequest
		case "$!!":
			return PacketTypeKillRequest
		case "$ZC":
			return PacketTypeAuthChallenge
		case "$HO":
			return PacketTypeHandoffRequest
		case "$HA":
			return PacketTypeHandoffAccept
		case "$FP":
			return PacketTypeFlightPlan
		case "$AM":
			return PacketTypeFlightPlanAmendment
		default:
			return PacketTypeUnknown
		}
	default:
		return PacketTypeUnknown
	}
}

func getPacketPrefix(packetType PacketType) string {
	switch packetType {
	case PacketTypePilotPositionFast:
		return "^"
	case PacketTypePilotPosition:
		return "@"
	case PacketTypeATCPosition:
		return "%"
	case PacketTypeDeleteATC:
		return "#DA"
	case PacketTypeDeletePilot:
		return "#DP"
	case PacketTypeTextMessage:
		return "#TM"
	case PacketTypePilotPositionSlow:
		return "#SL"
	case PacketTypePilotPositionStopped:
		return "#ST"
	case PacketTypeProController:
		return "#PC"
	case PacketTypeSquawkbox:
		return "#SB"
	case PacketTypeClientQuery:
		return "$CQ"
	case PacketTypeClientQueryResponse:
		return "$CR"
	case PacketTypeMetarRequest:
		return "$AX"
	case PacketTypeKillRequest:
		return "$!!"
	case PacketTypeAuthChallenge:
		return "$ZC"
	case PacketTypeHandoffRequest:
		return "$HO"
	case PacketTypeHandoffAccept:
		return "$HA"
	case PacketTypeFlightPlan:
		return "$FP"
	case PacketTypeFlightPlanAmendment:
		return "$AM"
	default:
		return ""
	}
}

func minFields(packetType PacketType) int {
	switch packetType {
	case PacketTypePilotPosition:
		return 9
	case PacketTypePilotPositionFast, PacketTypePilotPositionSlow:
		return 13
	case PacketTypePilotPositionStopped:
		return 7
	case PacketTypeATCPosition:
		return 7
	case PacketTypeDeleteATC, PacketTypeDeletePilot:
		return 1
	case PacketTypeTextMessage:
		return 3
	case PacketTypeProController:
		return 4
	case PacketTypeSquawkbox:
		return 3
	case PacketTypeClientQuery:
		return 3
	case PacketTypeClientQueryResponse:
		return 3
	case PacketTypeMetarRequest:
		return 4
	case PacketTypeKillRequest:
		return 3
	case PacketTypeAuthChallenge:
		return 3
	case PacketTypeHandoffRequest, PacketTypeHandoffAccept:
		return 3
	case PacketTypeFlightPlan:
		return 17
	case PacketTypeFlightPlanAmendment:
		return 18
	default:
		return -1
	}
}

type handlerFunc func(client *Client, packet []byte)

func getSourceCallsign(packet []byte, packetType PacketType) []byte {
	callsign, _ := bytes.CutPrefix(
		getField(packet, sourceCallsignFieldIndex(packetType)),
		[]byte(getPacketPrefix(packetType)),
	)
	return callsign
}

func verifySourceCallsign(packet []byte, packetType PacketType, callsign string) bool {
	sourceCallsign := getSourceCallsign(packet, packetType)
	return string(sourceCallsign) == callsign
}

// verifyPacket runs a set of sanity checks against a packet sent by a client and returns the detected packet type
func verifyPacket(packet []byte, client *Client) (packetType PacketType, ok bool) {
	numFields := countFields(packet)
	if len(packet) < 8 || numFields < 3 {
		client.sendError(SyntaxError, "Packet too short")
		return
	}

	packetType = getPacketType(packet)
	if packetType == PacketTypeUnknown {
		client.sendError(SyntaxError, "Unknown packet type")
		return
	}

	if !verifySourceCallsign(packet, packetType, client.callsign) {
		client.sendError(SourceInvalidError, "Source invalid")
		return
	}

	if numFields < minFields(packetType) {
		client.sendError(SyntaxError, "Minimum field count requirement not satisfied")
		return
	}

	ok = true
	return
}
