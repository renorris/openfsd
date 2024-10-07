package protocol

import (
	"fmt"
	"strings"
)

type PongPDU struct {
	From      string `validate:"required,alphanum,max=16"`
	To        string `validate:"required,alphanum,max=16"`
	Timestamp string `validate:"required,max=64"`
}

func (p *PongPDU) Serialize() string {
	return fmt.Sprintf("$PO%s:%s:%s%s", p.From, p.To, p.Timestamp, PacketDelimiter)
}

func (p *PongPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$PO")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := PongPDU{
		From:      fields[0],
		To:        fields[1],
		Timestamp: fields[2],
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
