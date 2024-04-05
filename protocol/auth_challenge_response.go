package protocol

import (
	"fmt"
	"strings"
)

type AuthChallengeResponsePDU struct {
	From              string `validate:"required,alphanum,max=7"`
	To                string `validate:"required,alphanum,max=7"`
	ChallengeResponse string `validate:"required,hexadecimal,md5"`
}

func (p *AuthChallengeResponsePDU) Serialize() string {
	return fmt.Sprintf("$ZR%s:%s:%s%s", p.From, p.To, p.ChallengeResponse, PacketDelimeter)
}

func ParseAuthChallengeResponsePDU(rawPacket string) (*AuthChallengeResponsePDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$ZR")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := AuthChallengeResponsePDU{
		From:              fields[0],
		To:                fields[1],
		ChallengeResponse: fields[2],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
