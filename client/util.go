package client

import (
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/renorris/openfsd/servercontext"
	"strings"
)

func (c *FSDClient) sendMOTD() error {
	lines := strings.Split(servercontext.Config().MOTD, "\n")
	for _, line := range lines {
		pdu := protocol.TextMessagePDU{
			From:    protocol.ServerCallsign,
			To:      c.callsign,
			Message: line,
		}
		if err := c.connection.WritePacket(pdu.Serialize()); err != nil {
			return err
		}
	}

	return nil
}

func (c *FSDClient) broadcastAddPilot() {
	p := protocol.AddPilotPDU{
		From:             c.callsign,
		To:               protocol.ServerCallsign,
		CID:              c.cid,
		NetworkRating:    c.networkRating,
		ProtocolRevision: protocol.ProtoRevisionVatsim2022,
		SimulatorType:    c.simulatorType,
		RealName:         c.realName,
	}
	mail := postoffice.NewMail(c, postoffice.MailTypeBroadcast, "", p.Serialize())
	servercontext.PostOffice().SendMail(&mail)
}

func (c *FSDClient) broadcastDeletePilot() {
	deletePilotPDU := protocol.DeletePilotPDU{
		From: c.callsign,
		CID:  c.cid,
	}
	mail := postoffice.NewMail(c, postoffice.MailTypeBroadcast, "", deletePilotPDU.Serialize())
	servercontext.PostOffice().SendMail(&mail)
}
