package protocol

import (
	"fmt"
	"strings"
)

type PlaneInfoRequestPDU struct {
	From string `validate:"required,alphanum,max=7"`
	To   string `validate:"required,alphanum,max=7"`
}

func (p *PlaneInfoRequestPDU) Serialize() string {
	return fmt.Sprintf("#SB%s:%s:PIR%s", p.From, p.To, PacketDelimeter)
}

func ParsePlaneInfoRequestPDU(rawPacket string) (*PlaneInfoRequestPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "#SB")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	if fields[2] != "PIR" {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := &PlaneInfoRequestPDU{
		From: fields[0],
		To:   fields[1],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return pdu, nil
}
