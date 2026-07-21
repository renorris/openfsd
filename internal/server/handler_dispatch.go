package server

import "github.com/renorris/openfsd/internal/session"

func (s *Server) getHandler(packetType PacketType) handlerFunc {
	switch packetType {
	case PacketTypeTextMessage:
		return s.handleTextMessage
	case PacketTypeATCPosition:
		return s.handleATCPosition
	case PacketTypeSecondaryVisCenter:
		return s.handleSecondaryVisCenter
	case PacketTypePilotPosition:
		return s.handlePilotPosition
	case PacketTypePilotPositionFast, PacketTypePilotPositionSlow, PacketTypePilotPositionStopped:
		return s.handleFastPilotPosition
	case PacketTypeDeletePilot, PacketTypeDeleteATC:
		return s.handleDelete
	case PacketTypeSquawkbox:
		return s.handleSquawkbox
	case PacketTypeProController:
		return s.handleProcontroller
	case PacketTypeClientQuery, PacketTypeClientQueryResponse:
		return s.handleClientQuery
	case PacketTypeKillRequest:
		return s.handleKillRequest
	case PacketTypeAuthChallenge:
		return s.handleAuthChallenge
	case PacketTypeHandoffRequest, PacketTypeHandoffAccept:
		return s.handleHandoff
	case PacketTypeMetarRequest:
		return s.handleMetarRequest
	case PacketTypeFlightPlan:
		return s.handleFileFlightplan
	case PacketTypeFlightPlanAmendment:
		return s.handleAmendFlightplan
	default:
		return s.emptyHandler
	}
}

func (s *Server) emptyHandler(client *session.Session, packet []byte) {
	s.logger.Error("empty handler called")
}
