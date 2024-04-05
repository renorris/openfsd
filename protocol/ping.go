package protocol

import (
	"fmt"
	"strings"
)

type PingPDU struct {
	From      string `validate:"required,alphanum,max=7"`
	To        string `validate:"required,alphanum,max=7"`
	Timestamp string `validate:"required,max=32"`
}

func (p *PingPDU) Serialize() string {
	return fmt.Sprintf("$PI%s:%s:%s%s", p.From, p.To, p.Timestamp, PacketDelimeter)
}

func ParsePingPDU(rawPacket string) (*PingPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$PI")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := PingPDU{
		From:      fields[0],
		To:        fields[1],
		Timestamp: fields[2],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
