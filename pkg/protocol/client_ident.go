package protocol

import (
	"strconv"
	"strings"
)

// ClientIdent is a $ID client identification packet.
type ClientIdent struct {
	Callsign        string
	To              string
	SoftwareID      string // hex-encoded uint16 as sent on the wire
	SoftwareName    string
	VersionMajor    int
	VersionMinor    int
	CID             int
	SystemUID       int
	ChallengeKey    string // optional; empty when omitted
	HasChallengeKey bool
}

// Marshal encodes the packet. When HasChallengeKey is true (or ChallengeKey
// is non-empty), the challenge field is appended.
func (p ClientIdent) Marshal() []byte {
	var b strings.Builder
	b.Grow(64 + len(p.Callsign) + len(p.SoftwareName) + len(p.ChallengeKey))
	b.WriteString("$ID")
	b.WriteString(p.Callsign)
	b.WriteByte(':')
	to := p.To
	if to == "" {
		to = "SERVER"
	}
	b.WriteString(to)
	b.WriteByte(':')
	b.WriteString(p.SoftwareID)
	b.WriteByte(':')
	b.WriteString(p.SoftwareName)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.VersionMajor))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.VersionMinor))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.CID))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.SystemUID))
	if p.HasChallengeKey || p.ChallengeKey != "" {
		b.WriteByte(':')
		b.WriteString(p.ChallengeKey)
	}
	b.WriteString("\r\n")
	return []byte(b.String())
}

// ParseClientIdent parses a $ID line. Trailing \r\n is optional.
// Accepts 8 fields (no challenge) or 9 fields (with challenge).
func ParseClientIdent(line []byte) (ClientIdent, error) {
	if TypeOf(line) != PacketTypeClientIdent {
		return ClientIdent{}, errPacket("client ident: wrong type")
	}
	n := CountFields(line)
	if n < MinFields(PacketTypeClientIdent) {
		return ClientIdent{}, errPacket("client ident: too few fields")
	}

	callsign, ok := cutPrefixField(Field(line, 0), Prefix(PacketTypeClientIdent))
	if !ok {
		return ClientIdent{}, errPacket("client ident: missing $ID prefix")
	}

	major, err := strconv.Atoi(string(Field(line, 4)))
	if err != nil {
		return ClientIdent{}, errPacket("client ident: bad major version")
	}
	minor, err := strconv.Atoi(string(Field(line, 5)))
	if err != nil {
		return ClientIdent{}, errPacket("client ident: bad minor version")
	}
	cid, err := strconv.Atoi(string(Field(line, 6)))
	if err != nil {
		return ClientIdent{}, errPacket("client ident: bad CID")
	}
	uid, err := strconv.Atoi(string(Field(line, 7)))
	if err != nil {
		return ClientIdent{}, errPacket("client ident: bad system UID")
	}

	p := ClientIdent{
		Callsign:     callsign,
		To:           string(Field(line, 1)),
		SoftwareID:   string(Field(line, 2)),
		SoftwareName: string(Field(line, 3)),
		VersionMajor: major,
		VersionMinor: minor,
		CID:          cid,
		SystemUID:    uid,
	}
	if n >= 9 {
		p.ChallengeKey = string(Field(line, 8))
		p.HasChallengeKey = true
	}
	return p, nil
}
