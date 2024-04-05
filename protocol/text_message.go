package protocol

import (
	"fmt"
	"strings"
)

type TextMessagePDU struct {
	From    string `validate:"required,alphanum,max=7"`
	To      string `validate:"required,max=7"`
	Message string `validate:"required"`
}

func (p *TextMessagePDU) Serialize() string {
	return fmt.Sprintf("#TM%s:%s:%s%s", p.From, p.To, p.Message, PacketDelimeter)
}

func ParseTextMessagePDU(rawPacket string) (*TextMessagePDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "#TM")
	fields := strings.SplitN(rawPacket, Delimeter, 3)

	if len(fields) < 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := TextMessagePDU{
		From:    fields[0],
		To:      fields[1],
		Message: fields[2],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
