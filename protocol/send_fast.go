package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type SendFastPDU struct {
	From       string `validate:"required,alphanum,max=16"`
	To         string `validate:"required,alphanum,max=16"`
	DoSendFast bool   `validate:""`
}

func (p *SendFastPDU) Serialize() string {
	var doSendFastInt int
	if p.DoSendFast {
		doSendFastInt = 1
	}
	return fmt.Sprintf("$SF%s:%s:%d%s", p.From, p.To, doSendFastInt, PacketDelimiter)
}

func (p *SendFastPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$SF")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 3 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := SendFastPDU{
		From: fields[0],
		To:   fields[1],
	}

	var doSendFastInt int
	var err error
	if doSendFastInt, err = strconv.Atoi(fields[2]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[2], "invalid send fast integer")
	}

	if doSendFastInt < 0 || doSendFastInt > 1 {
		return NewGenericFSDError(SyntaxError, fields[2], "send fast integer must be 1 or 0")
	}

	pdu.DoSendFast = doSendFastInt == 1

	if err = V.Struct(pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
