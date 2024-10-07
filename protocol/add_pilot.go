package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type AddPilotPDU struct {
	From             string        `validate:"required,alphanum,max=16"`
	To               string        `validate:"required,alphanum,max=16"`
	CID              int           `validate:"required,min=100000,max=9999999"`
	Token            string        `validate:"required"`
	NetworkRating    NetworkRating `validate:"min=1,max=12"`
	ProtocolRevision int           `validate:"min=0,max=101"`
	SimulatorType    int           `validate:"min=0,max=32"`
	RealName         string        `validate:"required,max=64"`
}

func (p *AddPilotPDU) Serialize() string {
	return fmt.Sprintf("#AP%s:%s:%d:%s:%d:%d:%d:%s%s",
		p.From, p.To, p.CID, p.Token, p.NetworkRating, p.ProtocolRevision,
		p.SimulatorType, p.RealName, PacketDelimiter)
}

func (p *AddPilotPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#AP")

	// Extract fields
	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 8 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := AddPilotPDU{
		From:     fields[0],
		To:       fields[1],
		Token:    fields[3],
		RealName: fields[7],
	}

	var err error

	// Parse integer fields
	if pdu.CID, err = strconv.Atoi(fields[2]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[2], "invalid CID")
	}

	var r int
	if r, err = strconv.Atoi(fields[4]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[4], "invalid network rating")
	}
	pdu.NetworkRating = NetworkRating(r)

	if pdu.ProtocolRevision, err = strconv.Atoi(fields[5]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[5], "invalid protocol revision")
	}

	if pdu.SimulatorType, err = strconv.Atoi(fields[6]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[6], "invalid simulator type")
	}

	// Validate
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
