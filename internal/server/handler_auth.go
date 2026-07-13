package server

import (
	"strings"

	"github.com/renorris/openfsd/internal/session"
)

func (s *Server) handleAuthChallenge(client *session.Session, packet []byte) {
	if client.ClientChallenge == "" {
		client.SendError(UnauthorizedSoftwareError, "Cannot reply to auth challenge since no initial challenge was recieved")
		return
	}

	challenge := getField(packet, 2)
	resp := client.Auth.GetResponseForChallenge(challenge)
	client.Auth.UpdateState(&resp)

	respPacket := strings.Builder{}
	respPacket.WriteString("$ZRSERVER:")
	respPacket.WriteString(client.Callsign)
	respPacket.WriteByte(':')
	respPacket.Write(resp[:])
	respPacket.WriteString("\r\n")

	client.Send(respPacket.String())
}

func (s *Server) handleHandoff(client *session.Session, packet []byte) {
	// Active >OBS ATC only
	if !client.IsAtc || client.FacilityType <= 1 {
		return
	}

	recipient := getField(packet, 1)
	sendDirectOrErr(s.registry, client, recipient, packet)
}
