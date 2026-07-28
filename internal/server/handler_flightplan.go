package server

import (
	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/session"
)

func (s *Server) handleFileFlightplan(client *session.Session, packet []byte) {
	if !s.rateOK(&client.LastFPLRateNs, minFPLInterval) {
		return
	}

	fplInfo := extractFlightplanInfoSection(packet)
	client.FlightPlan.Store(fplInfo)
	if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
		hr.Mesh().NotifyLocalFPL(client.Callsign, fplInfo)
	}

	broadcastPacket := buildFileFlightplanPacket(client.Callsign, "*A", fplInfo)
	broadcastAllATC(s.registry, client, []byte(broadcastPacket))
	if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
		hr.Mesh().BroadcastClass([]byte(broadcastPacket), cluster.BroadcastATC)
	}
}

func (s *Server) handleAmendFlightplan(client *session.Session, packet []byte) {
	if !client.IsAtc || client.FacilityType.Load() <= 0 {
		return
	}
	if !s.rateOK(&client.LastFPLRateNs, minFPLInterval) {
		return
	}

	fplInfo := extractFlightplanInfoSection(packet)

	targetCallsign := string(getField(packet, 2))
	targetClient, err := s.registry.Find(targetCallsign)
	if err == nil {
		targetClient.FlightPlan.Store(fplInfo)
		if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
			hr.Mesh().NotifyLocalFPL(targetCallsign, fplInfo)
		}
	} else if _, ok := s.registry.(*HybridRegistry); ok {
		// Async HomeRPC — never block gnet.
		s.meshHomeRPC(client, targetCallsign, cluster.HomeOpMutateFlightPlan, []byte(fplInfo),
			func(_ []byte) {
				broadcastPacket := buildAmendFlightplanPacket(client.Callsign, "*A", targetCallsign, fplInfo)
				broadcastAllATC(s.registry, client, []byte(broadcastPacket))
				if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
					hr.Mesh().BroadcastClass([]byte(broadcastPacket), cluster.BroadcastATC)
				}
			},
			func(err error) {
				client.SendError(NoSuchCallsignError, "No such callsign: "+targetCallsign)
			},
		)
		return
	} else {
		client.SendError(NoSuchCallsignError, "No such callsign: "+targetCallsign)
		return
	}

	broadcastPacket := buildAmendFlightplanPacket(client.Callsign, "*A", targetCallsign, fplInfo)
	broadcastAllATC(s.registry, client, []byte(broadcastPacket))
	if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
		hr.Mesh().BroadcastClass([]byte(broadcastPacket), cluster.BroadcastATC)
	}
}
