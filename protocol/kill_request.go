package protocol

import (
	"fmt"
	"strings"
)

type KillRequestPDU struct {
	From   string `validate:"required,alphanum,max=16"`
	To     string `validate:"required,alphanum,max=16"`
	Reason string `validate:"max=256"`
}

func (p *KillRequestPDU) Serialize() string {
	if p.Reason == "" {
		return fmt.Sprintf("$!!%s:%s%s", p.From, p.To, PacketDelimiter)
	} else {
		return fmt.Sprintf("$!!%s:%s:%s%s", p.From, p.To, p.Reason, PacketDelimiter)
	}
}

func (p *KillRequestPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$!!")

	var fields []string
	if fields = strings.SplitN(packet, Delimiter, 3); len(fields) < 2 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	var reason string
	if len(fields) == 3 {
		reason = fields[2]
	}

	pdu := KillRequestPDU{
		From:   fields[0],
		To:     fields[1],
		Reason: reason,
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
