package protocol

import "bytes"

// TypeOf detects the packet type from the wire prefix.
// Short or unknown packets return PacketTypeUnknown; never panics.
func TypeOf(packet []byte) PacketType {
	if len(packet) == 0 {
		return PacketTypeUnknown
	}

	switch packet[0] {
	case '^':
		return PacketTypePilotPositionFast
	case '@':
		return PacketTypePilotPosition
	case '%':
		return PacketTypeATCPosition
	case '#':
		if len(packet) < 3 {
			return PacketTypeUnknown
		}
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
		case "#AP":
			return PacketTypeAddPilot
		case "#AA":
			return PacketTypeAddATC
		default:
			return PacketTypeUnknown
		}
	case '$':
		if len(packet) < 3 {
			return PacketTypeUnknown
		}
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
		case "$DI":
			return PacketTypeServerIdent
		case "$ID":
			return PacketTypeClientIdent
		case "$ER":
			return PacketTypeError
		default:
			return PacketTypeUnknown
		}
	default:
		return PacketTypeUnknown
	}
}

// Prefix returns the wire prefix string for t, or "" if unknown.
func Prefix(t PacketType) string {
	switch t {
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
	case PacketTypeServerIdent:
		return "$DI"
	case PacketTypeClientIdent:
		return "$ID"
	case PacketTypeAddPilot:
		return "#AP"
	case PacketTypeAddATC:
		return "#AA"
	case PacketTypeError:
		return "$ER"
	default:
		return ""
	}
}

// MinFields returns the minimum colon-delimited field count for t.
// Returns -1 for unknown types.
func MinFields(t PacketType) int {
	switch t {
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
	case PacketTypeServerIdent:
		return 4
	case PacketTypeClientIdent:
		return 8
	case PacketTypeAddPilot:
		return 8
	case PacketTypeAddATC:
		return 7
	case PacketTypeError:
		return 5
	default:
		return -1
	}
}

// sourceCallsignFieldIndex returns the field index holding the source callsign.
func sourceCallsignFieldIndex(t PacketType) int {
	switch t {
	case PacketTypePilotPosition:
		return 1
	default:
		return 0
	}
}

// SourceCallsign extracts the source callsign bytes from packet for type t.
// The packet-type prefix is stripped from the field value.
func SourceCallsign(packet []byte, t PacketType) []byte {
	callsign, _ := bytes.CutPrefix(
		Field(packet, sourceCallsignFieldIndex(t)),
		[]byte(Prefix(t)),
	)
	return callsign
}

// VerifySourceCallsign reports whether the packet's source callsign equals callsign.
func VerifySourceCallsign(packet []byte, t PacketType, callsign string) bool {
	return string(SourceCallsign(packet, t)) == callsign
}
