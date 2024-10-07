package protocol

import (
	"fmt"
	"strings"
)

type AuthChallengeResponsePDU struct {
	From              string `validate:"required,alphanum,max=16"`
	To                string `validate:"required,alphanum,max=16"`
	ChallengeResponse string `validate:"required,hexadecimal,md5"`
}

func (p *AuthChallengeResponsePDU) Serialize() string {
	return fmt.Sprintf("$ZR%s:%s:%s%s", p.From, p.To, p.ChallengeResponse, PacketDelimiter)
}

func (p *AuthChallengeResponsePDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$ZR")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := AuthChallengeResponsePDU{
		From:              fields[0],
		To:                fields[1],
		ChallengeResponse: fields[2],
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
