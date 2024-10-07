package protocol

import (
	"fmt"
	"strings"
)

type AuthChallengePDU struct {
	From      string `validate:"required,alphanum,max=16"`
	To        string `validate:"required,alphanum,max=16"`
	Challenge string `validate:"required,hexadecimal,min=4,max=32"`
}

func (p *AuthChallengePDU) Serialize() string {
	return fmt.Sprintf("$ZC%s:%s:%s%s", p.From, p.To, p.Challenge, PacketDelimiter)
}

func (p *AuthChallengePDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$ZC")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := AuthChallengePDU{
		From:      fields[0],
		To:        fields[1],
		Challenge: fields[2],
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
