package protocol

import (
	"fmt"
	"strings"
)

type ClientQueryResponsePDU struct {
	From      string `validate:"required,alphanum,max=7"`
	To        string `validate:"required,max=7"`
	QueryType string `validate:"required,ascii,min=2,max=7"`
	Payload   string `validate:""`
}

func (p *ClientQueryResponsePDU) Serialize() string {
	if p.Payload == "" {
		return fmt.Sprintf("$CR%s:%s:%s%s", p.From, p.To, p.QueryType, PacketDelimeter)
	} else {
		return fmt.Sprintf("$CR%s:%s:%s:%s%s", p.From, p.To, p.QueryType, p.Payload, PacketDelimeter)
	}
}

func ParseClientQueryResponsePDU(rawPacket string) (*ClientQueryResponsePDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$CR")
	fields := strings.SplitN(rawPacket, Delimeter, 4)
	if len(fields) < 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	payload := ""
	if len(fields) == 4 {
		payload = fields[3]
	}

	pdu := ClientQueryResponsePDU{
		From:      fields[0],
		To:        fields[1],
		QueryType: fields[2],
		Payload:   payload,
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	switch pdu.QueryType {
	case "ATC", "CAPS", "C?", "RN", "SV", "ATIS", "IP", "INF", "FP", "IPC", "BY", "HI", "HLP", "NOHLP", "WH", "IT", "HT", "DR", "FA", "TA", "BC", "SC", "VT", "ACC", "NEWINFO", "NEWATIS", "EST", "GD":
	default:
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
