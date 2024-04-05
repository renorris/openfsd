package protocol

import (
	"fmt"
	"strings"
)

type AuthChallengePDU struct {
	From      string `validate:"required,alphanum,max=7"`
	To        string `validate:"required,alphanum,max=7"`
	Challenge string `validate:"required,hexadecimal,min=4,max=32"`
}

func (p *AuthChallengePDU) Serialize() string {
	return fmt.Sprintf("$ZC%s:%s:%s%s", p.From, p.To, p.Challenge, PacketDelimeter)
}

func ParseAuthChallengePDU(rawPacket string) (*AuthChallengePDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$ZC")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := AuthChallengePDU{
		From:      fields[0],
		To:        fields[1],
		Challenge: fields[2],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
