package server

import "github.com/renorris/openfsd/internal/session"

// handleDelete handles logic for Delete ATC `#DA` and Delete Pilot `#DP` packets
func (s *Server) handleDelete(client *session.Session, packet []byte) {
	// Broadcast delete packet
	broadcastAll(s.registry, client, packet)

	// Cancel context. Writer worker will close the connection
	client.Cancel()
}
