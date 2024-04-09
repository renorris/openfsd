package protocol

import (
	"fmt"
	"strings"
)

type MetarRequestPDU struct {
	From    string `validate:"required,alphanum,max=7"`
	To      string `validate:"required,alphanum,max=7"`
	Station string `validate:"required,alphanum,max=4"`
}

func (p *MetarRequestPDU) Serialize() string {
	return fmt.Sprintf("$AX%s:%s:METAR:%s%s", p.From, p.To, p.Station, PacketDelimeter)
}

func ParseMetarRequestPDU(rawPacket string) (*MetarRequestPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$AX")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 4 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	if fields[2] != "METAR" {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := MetarRequestPDU{
		From:    fields[0],
		To:      fields[1],
		Station: fields[3],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
