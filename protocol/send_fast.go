package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type SendFastPDU struct {
	From       string `validate:"required,alphanum,max=7"`
	To         string `validate:"required,alphanum,max=7"`
	DoSendFast bool   `validate:""`
}

func (p *SendFastPDU) Serialize() string {
	var doSendFastInt int
	if p.DoSendFast {
		doSendFastInt = 1
	}
	return fmt.Sprintf("$SF%s:%s:%d%s", p.From, p.To, doSendFastInt, PacketDelimeter)
}

func ParseSendFastPDU(rawPacket string) (*SendFastPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$SF")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 3 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	doSendFastInt, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}
	doSendFast := doSendFastInt == 1

	pdu := SendFastPDU{
		From:       fields[0],
		To:         fields[1],
		DoSendFast: doSendFast,
	}

	err = V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
