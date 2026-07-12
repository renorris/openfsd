package protocol

import (
	"strconv"
	"strings"
)

// AddPilot is a #AP pilot login / add packet.
type AddPilot struct {
	Callsign      string
	To            string
	CID           string
	Token         string
	NetworkRating NetworkRating
	ProtoRevision int
	SimulatorType int
	RealName      string
}

// Marshal encodes the #AP packet.
func (p AddPilot) Marshal() []byte {
	var b strings.Builder
	b.Grow(64 + len(p.Callsign) + len(p.Token) + len(p.RealName))
	b.WriteString("#AP")
	b.WriteString(p.Callsign)
	b.WriteByte(':')
	to := p.To
	if to == "" {
		to = "SERVER"
	}
	b.WriteString(to)
	b.WriteByte(':')
	b.WriteString(p.CID)
	b.WriteByte(':')
	b.WriteString(p.Token)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(p.NetworkRating)))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.ProtoRevision))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.SimulatorType))
	b.WriteByte(':')
	b.WriteString(p.RealName)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// ParseAddPilot parses a #AP line.
func ParseAddPilot(line []byte) (AddPilot, error) {
	if TypeOf(line) != PacketTypeAddPilot {
		return AddPilot{}, errPacket("add pilot: wrong type")
	}
	if CountFields(line) < MinFields(PacketTypeAddPilot) {
		return AddPilot{}, errPacket("add pilot: too few fields")
	}
	callsign, ok := cutPrefixField(Field(line, 0), Prefix(PacketTypeAddPilot))
	if !ok {
		return AddPilot{}, errPacket("add pilot: missing #AP prefix")
	}
	rating, err := strconv.Atoi(string(Field(line, 4)))
	if err != nil {
		return AddPilot{}, errPacket("add pilot: bad network rating")
	}
	proto, err := strconv.Atoi(string(Field(line, 5)))
	if err != nil {
		return AddPilot{}, errPacket("add pilot: bad protocol revision")
	}
	sim, err := strconv.Atoi(string(Field(line, 6)))
	if err != nil {
		return AddPilot{}, errPacket("add pilot: bad simulator type")
	}
	return AddPilot{
		Callsign:      callsign,
		To:            string(Field(line, 1)),
		CID:           string(Field(line, 2)),
		Token:         string(Field(line, 3)),
		NetworkRating: NetworkRating(rating),
		ProtoRevision: proto,
		SimulatorType: sim,
		RealName:      string(Field(line, 7)),
	}, nil
}

// AddATC is a #AA ATC login / add packet.
type AddATC struct {
	Callsign      string
	To            string
	RealName      string
	CID           string
	Token         string
	NetworkRating NetworkRating
	ProtoRevision int
}

// Marshal encodes the #AA packet.
func (p AddATC) Marshal() []byte {
	var b strings.Builder
	b.Grow(64 + len(p.Callsign) + len(p.Token) + len(p.RealName))
	b.WriteString("#AA")
	b.WriteString(p.Callsign)
	b.WriteByte(':')
	to := p.To
	if to == "" {
		to = "SERVER"
	}
	b.WriteString(to)
	b.WriteByte(':')
	b.WriteString(p.RealName)
	b.WriteByte(':')
	b.WriteString(p.CID)
	b.WriteByte(':')
	b.WriteString(p.Token)
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(int(p.NetworkRating)))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(p.ProtoRevision))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// ParseAddATC parses a #AA line.
func ParseAddATC(line []byte) (AddATC, error) {
	if TypeOf(line) != PacketTypeAddATC {
		return AddATC{}, errPacket("add atc: wrong type")
	}
	if CountFields(line) < MinFields(PacketTypeAddATC) {
		return AddATC{}, errPacket("add atc: too few fields")
	}
	callsign, ok := cutPrefixField(Field(line, 0), Prefix(PacketTypeAddATC))
	if !ok {
		return AddATC{}, errPacket("add atc: missing #AA prefix")
	}
	rating, err := strconv.Atoi(string(Field(line, 5)))
	if err != nil {
		return AddATC{}, errPacket("add atc: bad network rating")
	}
	proto, err := strconv.Atoi(string(Field(line, 6)))
	if err != nil {
		return AddATC{}, errPacket("add atc: bad protocol revision")
	}
	return AddATC{
		Callsign:      callsign,
		To:            string(Field(line, 1)),
		RealName:      string(Field(line, 2)),
		CID:           string(Field(line, 3)),
		Token:         string(Field(line, 4)),
		NetworkRating: NetworkRating(rating),
		ProtoRevision: proto,
	}, nil
}
