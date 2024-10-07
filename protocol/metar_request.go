package protocol

import (
	"fmt"
	"strings"
)

type MetarRequestPDU struct {
	From    string `validate:"required,alphanum,max=16"`
	To      string `validate:"required,alphanum,max=16"`
	Station string `validate:"required,alphanum,max=4"`
}

func (p *MetarRequestPDU) Serialize() string {
	return fmt.Sprintf("$AX%s:%s:METAR:%s%s", p.From, p.To, p.Station, PacketDelimiter)
}

func (p *MetarRequestPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$AX")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 4 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	if fields[2] != "METAR" {
		return NewGenericFSDError(SyntaxError, fields[2], "third parameter must be 'METAR'")
	}

	pdu := MetarRequestPDU{
		From:    fields[0],
		To:      fields[1],
		Station: fields[3],
	}

	if err := V.Struct(pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
