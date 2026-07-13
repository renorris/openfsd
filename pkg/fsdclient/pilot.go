package fsdclient

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/renorris/openfsd/pkg/protocol"
)

// PrefixSendFast is the wire prefix for the server "Send Fast Positions" packet
// ($SF). pkg/protocol.TypeOf does not classify $SF yet (returns Unknown); use
// IsSendFast to observe it from Next/WaitFor in proto-101 e2e.
const PrefixSendFast = "$SF"

// IsSendFast reports whether raw is a $SF (send-fast-positions) packet.
// Trailing \r\n is ignored.
func IsSendFast(raw []byte) bool {
	raw = bytes.TrimRight(raw, "\r\n")
	return bytes.HasPrefix(raw, []byte(PrefixSendFast))
}

// SendPilotPosition marshals and sends a protocol 100 @ position update.
func (c *Client) SendPilotPosition(pos protocol.PilotPosition) error {
	return c.Send(pos.Marshal())
}

// FastPilotPosition is a protocol-101 ^ visual position update.
// Thin local helper — not part of pkg/protocol v1.
type FastPilotPosition struct {
	Callsign         string
	Latitude         float64
	Longitude        float64
	TrueAltitude     float64
	AltitudeAGL      float64
	PitchBankHeading uint32
	VelX, VelY, VelZ float64
	RotX, RotY, RotZ float64
	NoseGearAngle    float64
}

// Marshal encodes the ^ packet with \r\n.
func (p FastPilotPosition) Marshal() []byte {
	var b strings.Builder
	b.Grow(128 + len(p.Callsign))
	b.WriteByte('^')
	b.WriteString(p.Callsign)
	b.WriteByte(':')
	b.WriteString(formatFloat(p.Latitude))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.Longitude))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.TrueAltitude))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.AltitudeAGL))
	b.WriteByte(':')
	b.WriteString(strconv.FormatUint(uint64(p.PitchBankHeading), 10))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.VelX))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.VelY))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.VelZ))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.RotX))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.RotY))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.RotZ))
	b.WriteByte(':')
	b.WriteString(formatFloat(p.NoseGearAngle))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// SendFastPosition sends a protocol-101 ^ position update.
func (c *Client) SendFastPosition(pos FastPilotPosition) error {
	return c.Send(pos.Marshal())
}

// SendSlowFastPosition sends a raw #SL (proto 101 slow visual) packet.
// packet may be the full wire line with or without trailing CRLF.
func (c *Client) SendSlowFastPosition(packet []byte) error {
	return c.Send(ensurePrefix(packet, "#SL"))
}

// SendStoppedPosition sends a raw #ST (proto 101 stopped visual) packet.
func (c *Client) SendStoppedPosition(packet []byte) error {
	return c.Send(ensurePrefix(packet, "#ST"))
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func ensurePrefix(packet []byte, prefix string) []byte {
	p := packet
	// Strip existing CRLF for prefix check.
	for len(p) > 0 && (p[len(p)-1] == '\n' || p[len(p)-1] == '\r') {
		p = p[:len(p)-1]
	}
	if len(p) >= len(prefix) && string(p[:len(prefix)]) == prefix {
		return packet
	}
	out := make([]byte, 0, len(prefix)+len(p)+2)
	out = append(out, prefix...)
	out = append(out, p...)
	return out
}
