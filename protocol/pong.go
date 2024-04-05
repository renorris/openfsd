package protocol

import (
	"fmt"
	"strings"
)

type PongPDU struct {
	From      string `validate:"required,alphanum,max=7"`
	To        string `validate:"required,alphanum,max=7"`
	Timestamp string `validate:"required,max=32"`
}

func (p *PongPDU) Serialize() string {
	return fmt.Sprintf("$PO%s:%s:%s%s", p.From, p.To, p.Timestamp, PacketDelimeter)
}

func ParsePongPDU(rawPacket string) (*PongPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$PO")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := PongPDU{
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
