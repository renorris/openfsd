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

	if string(recipient) == "FP" {
		// TODO: handle FP
		return
	}

	if string(recipient) == "SERVER" {
		// TODO: handle SERVER
		return
	}

	// Otherwise, treat as direct message
	sendDirectOrErr(s.registry, client, recipient, packet)
}
