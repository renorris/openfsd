package server

import "github.com/renorris/openfsd/internal/session"

func (s *Server) handleFileFlightplan(client *session.Session, packet []byte) {
	fplInfo := extractFlightplanInfoSection(packet)
	client.FlightPlan.Store(fplInfo)

	broadcastPacket := buildFileFlightplanPacket(client.Callsign, "*A", fplInfo)
	broadcastAllATC(s.registry, client, []byte(broadcastPacket))
}

func (s *Server) handleAmendFlightplan(client *session.Session, packet []byte) {
	if !client.IsAtc || client.FacilityType <= 0 {
		return
	}

	fplInfo := extractFlightplanInfoSection(packet)

	targetCallsign := string(getField(packet, 2))
	targetClient, err := s.registry.Find(targetCallsign)
	if err != nil {
		client.SendError(NoSuchCallsignError, "No such callsign: "+targetCallsign)
		return
	}
	targetClient.FlightPlan.Store(fplInfo)

	broadcastPacket := buildAmendFlightplanPacket(client.Callsign, "*A", targetCallsign, fplInfo)
	broadcastAllATC(s.registry, client, []byte(broadcastPacket))
}
