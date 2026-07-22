package server

import "github.com/renorris/openfsd/internal/session"

// handleDelete handles logic for Delete ATC `#DA` and Delete Pilot `#DP` packets
func (s *Server) handleDelete(client *session.Session, packet []byte) {
	// Broadcast the client's own delete packet once. Mark disconnect notified so
	// the connection-exit path (broadcastDisconnectPacket) does not emit a second #DA/#DP.
	broadcastAll(s.registry, client, packet)
	client.DisconnectNotified.Store(true)

	// Cancel context. Writer worker will close the connection
	client.Cancel()
}
