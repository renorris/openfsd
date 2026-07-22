package server

import (
	"bytes"

	"github.com/renorris/openfsd/internal/session"
)

func (s *Server) handleTextMessage(client *session.Session, packet []byte) {
	recipient := getField(packet, 1)

	// ATC chat
	if string(recipient) == "@49999" {
		if !client.IsAtc {
			return
		}
		broadcastRangedAtcOnly(s.registry, client, packet)
		return
	}

	// Frequency message
	if bytes.HasPrefix(recipient, []byte("@")) {
		broadcastRanged(s.registry, client, packet)
		return
	}

	// Wallop
	if string(recipient) == "*S" {
		broadcastAllSupervisors(s.registry, client, packet)
		return
	}

	// Server-wide broadcast message
	if string(recipient) == "*" {
		if client.NetworkRating < NetworkRatingSupervisor {
			return
		}
		broadcastAll(s.registry, client, packet)
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
