package server

import "github.com/renorris/openfsd/internal/session"

// handleDelete handles logic for Delete ATC `#DA` and Delete Pilot `#DP` packets.
// Always emits a server-built leave notification (session IsAtc + CID) rather than
// rebroadcasting the client packet (which may forge leave type or CID).
func (s *Server) handleDelete(client *session.Session, packet []byte) {
	_ = packet
	// broadcastDisconnectPacket is idempotent via DisconnectNotified CAS.
	s.broadcastDisconnectPacket(client)
	client.Disconnect()
}
