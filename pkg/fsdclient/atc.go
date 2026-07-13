package fsdclient

import "github.com/renorris/openfsd/pkg/protocol"

// SendATCPosition marshals and sends a % ATC position update.
func (c *Client) SendATCPosition(pos protocol.ATCPosition) error {
	return c.Send(pos.Marshal())
}

// SendDeletePilot sends a #DP disconnect notification for a pilot callsign.
// Wire form: #DP{callsign}:{cid}\r\n
func (c *Client) SendDeletePilot(callsign, cid string) error {
	return c.Send([]byte("#DP" + callsign + ":" + cid))
}

// SendDeleteATC sends a #DA disconnect notification for an ATC callsign.
// Wire form: #DA{callsign}:{cid}\r\n
func (c *Client) SendDeleteATC(callsign, cid string) error {
	return c.Send([]byte("#DA" + callsign + ":" + cid))
}
