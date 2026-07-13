package server

import "github.com/renorris/openfsd/internal/session"

func (s *Server) handleMetarRequest(client *session.Session, packet []byte) {
	recipient := getField(packet, 1)
	staticField := getField(packet, 2)
	icaoCode := getField(packet, 3)

	if string(recipient) != "SERVER" || string(staticField) != "METAR" {
		return
	}

	s.metar.Request(client.Ctx, client, string(icaoCode))
}
