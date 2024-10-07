package protocol

import (
	"fmt"
	"strings"
)

type ServerIdentificationPDU struct {
	From             string `validate:"required,alphanum,max=16"`
	To               string `validate:"required,alphanum,max=16"`
	Version          string `validate:"required,max=32"`
	InitialChallenge string `validate:"required,hexadecimal,max=32"`
}

func (p *ServerIdentificationPDU) Serialize() string {
	return fmt.Sprintf("$DI%s:%s:%s:%s%s", p.From, p.To, p.Version, p.InitialChallenge, PacketDelimiter)
}

func (p *ServerIdentificationPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$DI")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 4 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := ServerIdentificationPDU{
		From:             fields[0],
		To:               fields[1],
		Version:          fields[2],
		InitialChallenge: fields[3],
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
