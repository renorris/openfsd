package protocol

import (
	"fmt"
	"strings"
)

type TextMessagePDU struct {
	From    string `validate:"required,alphanum,max=16"`
	To      string `validate:"required,max=16"`
	Message string `validate:"required"`
}

func (p *TextMessagePDU) Serialize() string {
	return fmt.Sprintf("#TM%s:%s:%s%s", p.From, p.To, p.Message, PacketDelimiter)
}

func (p *TextMessagePDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#TM")

	var fields []string
	if fields = strings.SplitN(packet, Delimiter, 3); len(fields) < 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := TextMessagePDU{
		From:    fields[0],
		To:      fields[1],
		Message: fields[2],
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
