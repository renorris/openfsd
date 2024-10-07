package protocol

import (
	"fmt"
	"strings"
)

type PlaneInfoRequestPDU struct {
	From string `validate:"required,alphanum,max=16"`
	To   string `validate:"required,alphanum,max=16"`
}

func (p *PlaneInfoRequestPDU) Serialize() string {
	return fmt.Sprintf("#SB%s:%s:PIR%s", p.From, p.To, PacketDelimiter)
}

func (p *PlaneInfoRequestPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#SB")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	if fields[2] != "PIR" {
		return NewGenericFSDError(SyntaxError, fields[2], "third parameter must be 'PIR'")
	}

	pdu := PlaneInfoRequestPDU{
		From: fields[0],
		To:   fields[1],
	}

	if err := V.Struct(&pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
