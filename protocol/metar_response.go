package protocol

import (
	"fmt"
	"strings"
)

type MetarResponsePDU struct {
	From  string `validate:"required,alphanum,max=7"`
	To    string `validate:"required,alphanum,max=7"`
	Metar string `validate:"required,max=256"`
}

func (p *MetarResponsePDU) Serialize() string {
	return fmt.Sprintf("$AR%s:%s:%s%s", p.From, p.To, p.Metar, PacketDelimeter)
}

func ParseMetarResponsePDU(rawPacket string) (*MetarResponsePDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$AR")
	fields := strings.SplitN(rawPacket, Delimeter, 3)

	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := MetarResponsePDU{
		From:  fields[0],
		To:    fields[1],
		Metar: fields[2],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
