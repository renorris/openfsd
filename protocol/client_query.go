package protocol

import (
	"fmt"
	"slices"
	"strings"
)

var supportedClientQueryTypes = buildSupportedClientQueryTypes()

func buildSupportedClientQueryTypes() []string {
	typesList := []string{
		"ATC", "CAPS", "C?",
		"RN", "SV", "ATIS",
		"IP", "INF", "FP",
		"IPC", "BY", "HI",
		"HLP", "NOHLP", "WH",
		"IT", "HT", "DR",
		"FA", "TA", "BC",
		"SC", "VT", "ACC",
		"NEWINFO", "NEWATIS", "EST",
		"GD", "SIMDATA"}

	slices.Sort(typesList)
	return typesList
}

type ClientQueryPDU struct {
	From      string `validate:"required,alphanum,max=16"`
	To        string `validate:"required,max=7"`
	QueryType string `validate:"required,ascii,min=2,max=16"`
	Payload   string `validate:""`
}

func (p *ClientQueryPDU) Serialize() string {
	if p.Payload == "" {
		return fmt.Sprintf("$CQ%s:%s:%s%s",
			p.From, p.To, p.QueryType, PacketDelimiter)
	} else {
		return fmt.Sprintf("$CQ%s:%s:%s:%s%s",
			p.From, p.To, p.QueryType, p.Payload, PacketDelimiter)
	}
}

func (p *ClientQueryPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$CQ")

	// Extract fields
	var fields []string
	if fields = strings.SplitN(packet, Delimiter, 4); len(fields) < 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := ClientQueryPDU{
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

	if _, exists := slices.BinarySearch(supportedClientQueryTypes, pdu.QueryType); !exists {
		return NewGenericFSDError(SyntaxError, fields[2], "invalid query type")
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
