package protocol

import (
	"fmt"
	"strings"
)

type ServerIdentificationPDU struct {
	From             string `validate:"required,alphanum,max=7"`
	To               string `validate:"required,alphanum,max=7"`
	Version          string `validate:"required,max=32"`
	InitialChallenge string `validate:"required,hexadecimal,max=32"`
}

func (p *ServerIdentificationPDU) Serialize() string {
	return fmt.Sprintf("$DI%s:%s:%s:%s%s", p.From, p.To, p.Version, p.InitialChallenge, PacketDelimeter)
}

func ParseServerIdentificationPDU(rawPacket string) (*ServerIdentificationPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$DI")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) != 4 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := &ServerIdentificationPDU{
		From:             fields[0],
		To:               fields[1],
		Version:          fields[2],
		InitialChallenge: fields[3],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return pdu, nil
}
