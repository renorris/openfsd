package server

import "github.com/renorris/openfsd/internal/session"

func (s *Server) handleKillRequest(client *session.Session, packet []byte) {
	if client.NetworkRating < NetworkRatingSupervisor {
		return
	}

	// Attempt to find the victim client
	recipient := getField(packet, 1)
	victim, err := s.registry.Find(string(recipient))
	if err != nil {
		client.SendError(NoSuchCallsignError, "No such callsign")
		return
	}

	// Synthetic (sweatbox) sessions have no gnet disconnect Release defer — Remove
	// performs pointer-scoped #DP + registry.Release + engine.Delete.
	if victim.Synthetic && s.sweatbox != nil {
		_ = s.sweatbox.Remove(victim.Callsign)
		return
	}

	// Closing the context + transport forces disconnect on classic and gnet.
	victim.Disconnect()
}
