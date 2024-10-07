package protocol

import (
	"fmt"
	"strings"
)

type MetarResponsePDU struct {
	From  string `validate:"required,alphanum,max=16"`
	To    string `validate:"required,alphanum,max=16"`
	Metar string `validate:"required,max=512"`
}

func (p *MetarResponsePDU) Serialize() string {
	return fmt.Sprintf("$AR%s:%s:%s%s", p.From, p.To, p.Metar, PacketDelimiter)
}

func (p *MetarResponsePDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$AR")

	var fields []string
	if fields = strings.SplitN(packet, Delimiter, 3); len(fields) != 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := MetarResponsePDU{
		From:  fields[0],
		To:    fields[1],
		Metar: fields[2],
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
