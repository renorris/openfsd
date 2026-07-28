package server

import (
	"bytes"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/session"
)

func (s *Server) handleTextMessage(client *session.Session, packet []byte) {
	if !s.rateOK(&client.LastTextRateNs, minTextInterval) {
		return
	}

	recipient := getField(packet, 1)

	// ATC chat (@49999)
	// Design matrix: ATC-wide flood (BroadcastClass ATC), not interest-ranged.
	// Intentional: all-ATC mesh delivery so controllers see chat regardless of geo
	// interest boxes (R3-5). Local fan-out remains ranged ATC-only for density.
	if string(recipient) == "@49999" {
		if !client.IsAtc {
			return
		}
		broadcastRangedAtcOnly(s.registry, client, packet)
		if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
			hr.Mesh().BroadcastClass(packet, cluster.BroadcastATC)
		}
		return
	}

	// Frequency message
	if bytes.HasPrefix(recipient, []byte("@")) {
		broadcastRanged(s.registry, client, packet)
		s.meshForwardRangedText(client, packet)
		return
	}

	// Wallop — rate-limited to reduce supervisor spam
	if string(recipient) == "*S" {
		if !s.rateOK(&client.LastWallopRateNs, minWallopInterval) {
			return
		}
		broadcastAllSupervisors(s.registry, client, packet)
		if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
			hr.Mesh().BroadcastClass(packet, cluster.BroadcastSupervisor)
		}
		return
	}

	// Server-wide broadcast message
	if string(recipient) == "*" {
		if client.NetworkRating < NetworkRatingSupervisor {
			return
		}
		broadcastAll(s.registry, client, packet)
		if hr, ok := s.registry.(*HybridRegistry); ok && hr.Mesh() != nil {
			hr.Mesh().BroadcastClass(packet, cluster.BroadcastAll)
		}
		return
	}

	// FP is a pseudo-recipient used by ATC clients for flight-plan/track
	// acknowledgements (e.g. vatSys drop-track "#TM …:FP:{cs} release").
	// No server-side consumer; drop silently.
	if string(recipient) == "FP" {
		return
	}

	// SERVER text: openfsd has no interactive server chat/commands over #TM.
	// MOTD is pushed at login. Drop silently (do not $ER).
	if string(recipient) == "SERVER" {
		return
	}

	// Otherwise, treat as direct message
	sendDirectOrErr(s.registry, client, recipient, packet)
}

// meshForwardRangedText uses reliable text frames (not PositionBatch coalesce) — R2-5.
func (s *Server) meshForwardRangedText(client *session.Session, packet []byte) {
	hr, ok := s.registry.(*HybridRegistry)
	if !ok || hr.Mesh() == nil {
		return
	}
	boxes := sessionSenderBoxes(client)
	hr.Mesh().ForwardTextRanged(packet, boxes)
}
