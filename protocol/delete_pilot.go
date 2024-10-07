package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type DeletePilotPDU struct {
	From string `validate:"required,alphanum,max=16"`
	CID  int    `validate:"required,min=100000,max=9999999"`
}

func (p *DeletePilotPDU) Serialize() string {
	return fmt.Sprintf("#DP%s:%d%s", p.From, p.CID, PacketDelimiter)
}

func (p *DeletePilotPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#DP")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 2 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := DeletePilotPDU{
		From: fields[0],
	}

	var err error
	if pdu.CID, err = strconv.Atoi(fields[1]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[1], "invalid CID")
	}

	if err = V.Struct(&pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
