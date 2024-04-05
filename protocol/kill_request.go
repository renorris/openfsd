package protocol

import (
	"fmt"
	"strings"
)

type KillRequestPDU struct {
	From   string `validate:"required,alphanum,max=7"`
	To     string `validate:"required,alphanum,max=7"`
	Reason string `validate:"max=256"`
}

func (p *KillRequestPDU) Serialize() string {
	if p.Reason == "" {
		return fmt.Sprintf("$!!%s:%s%s", p.From, p.To, PacketDelimeter)
	} else {
		return fmt.Sprintf("$!!%s:%s:%s%s", p.From, p.To, p.Reason, PacketDelimeter)
	}
}

func ParseKillRequestPDU(rawPacket string) (*KillRequestPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$!!")
	fields := strings.SplitN(rawPacket, Delimeter, 3)

	if len(fields) < 2 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	var reason string
	if len(fields) == 3 {
		reason = fields[2]
	} else {
		reason = ""
	}

	pdu := KillRequestPDU{
		From:   fields[0],
		To:     fields[1],
		Reason: reason,
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
