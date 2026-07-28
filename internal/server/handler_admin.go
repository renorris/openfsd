package server

import (
	"github.com/renorris/openfsd/internal/session"
)

func (s *Server) handleKillRequest(client *session.Session, packet []byte) {
	if client.NetworkRating < NetworkRatingSupervisor {
		return
	}

	// Attempt to find the victim client
	recipient := getField(packet, 1)
	cs := string(recipient)
	victim, err := s.registry.Find(cs)
	if err != nil {
		if _, ok := s.registry.(*HybridRegistry); ok {
			// R2-4 / R2-11: rating checked on origin (supervisor gate above);
			// HomeRPC off gnet via authPool.
			s.meshForceDisconnectAsync(client, cs)
			return
		}
		client.SendError(NoSuchCallsignError, "No such callsign")
		return
	}

	// Synthetic (sweatbox) sessions have no gnet disconnect Release defer — Remove
	// performs pointer-scoped #DP + registry.Release + engine.Delete.
	if victim.Synthetic && s.sweatbox != nil {
		_ = s.sweatbox.Remove(victim.Callsign)
		return
	}

	// Closing the context + transport forces disconnect (gnet outbound / synthetic channel path).
	victim.Disconnect()
}
