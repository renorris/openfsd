package protocol

import (
	"fmt"
	"strings"
)

type ClientQueryResponsePDU struct {
	From      string `validate:"required,alphanum,max=16"`
	To        string `validate:"required,max=16"`
	QueryType string `validate:"required,ascii,min=2,max=7"`
	Payload   string `validate:""`
}

func (p *ClientQueryResponsePDU) Serialize() string {
	if p.Payload == "" {
		return fmt.Sprintf("$CR%s:%s:%s%s",
			p.From, p.To, p.QueryType, PacketDelimiter)
	} else {
		return fmt.Sprintf("$CR%s:%s:%s:%s%s",
			p.From, p.To, p.QueryType, p.Payload, PacketDelimiter)
	}
}

func (p *ClientQueryResponsePDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$CR")

	// Extract fields
	var fields []string
	if fields = strings.SplitN(packet, Delimiter, 4); len(fields) < 3 {
		return NewGenericFSDError(SyntaxError, "",
			"invalid parameter count")
	}

	pdu := ClientQueryResponsePDU{
		From:      fields[0],
		To:        fields[1],
		QueryType: fields[2],
	}

	if len(fields) == 4 {
		pdu.Payload = fields[3]
	}

	if err := V.Struct(&pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	switch pdu.QueryType {
	case "ATC", "CAPS", "C?",
		"RN", "SV", "ATIS",
		"IP", "INF", "FP",
		"IPC", "BY", "HI",
		"HLP", "NOHLP", "WH",
		"IT", "HT", "DR",
		"FA", "TA", "BC",
		"SC", "VT", "ACC",
		"NEWINFO", "NEWATIS", "EST",
		"GD":
	default:
		return NewGenericFSDError(SyntaxError, fields[2], "invalid query type")
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
