package protocol

import (
	"fmt"
	"strings"
)

type PlaneInfoResponsePDU struct {
	From      string `validate:"required,alphanum,max=16"`
	To        string `validate:"required,alphanum,max=16"`
	Equipment string `validate:"required,max=64"`
	Airline   string `validate:"max=64"`
	Livery    string `validate:"max=64"`
	CSL       string `validate:"max=64"`
}

func (p *PlaneInfoResponsePDU) Serialize() string {
	str := fmt.Sprintf("#SB%s:%s:PI:GEN:EQUIPMENT=%s", p.From, p.To, p.Equipment)
	if p.Airline != "" {
		str += fmt.Sprintf(":AIRLINE=%s", p.Airline)
	}
	if p.Livery != "" {
		str += fmt.Sprintf(":LIVERY=%s", p.Livery)
	}
	if p.CSL != "" {
		str += fmt.Sprintf(":CSL=%s", p.CSL)
	}

	str += PacketDelimiter
	return str
}

func (p *PlaneInfoResponsePDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#SB")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) < 5 || len(fields) > 8 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	if fields[2] != "PI" || fields[3] != "GEN" {
		return NewGenericFSDError(SyntaxError, fields[2], "third parameter must be 'PI'")
	}

	if fields[3] != "GEN" {
		return NewGenericFSDError(SyntaxError, fields[3], "fourth parameter must be 'GEN'")
	}

	pdu := PlaneInfoResponsePDU{
		From: fields[0],
		To:   fields[1],
	}

	if !strings.HasPrefix(fields[4], "EQUIPMENT=") || len(fields[4]) < len("EQUIPMENT=")+1 {
		return NewGenericFSDError(SyntaxError, fields[4], "invalid EQUIPMENT= field")
	}
	pdu.Equipment = strings.SplitN(fields[4], "=", 2)[1]

	if len(fields) > 5 {
		if !strings.HasPrefix(fields[5], "AIRLINE=") || len(fields[5]) < len("AIRLINE=")+1 {
			return NewGenericFSDError(SyntaxError, fields[5], "invalid AIRLINE= field")
		}
		pdu.Airline = strings.SplitN(fields[5], "=", 2)[1]
	}

	if len(fields) > 6 {
		if !strings.HasPrefix(fields[6], "LIVERY=") || len(fields[6]) < len("LIVERY=")+1 {
			return NewGenericFSDError(SyntaxError, fields[6], "invalid LIVERY= field")
		}
		pdu.Livery = strings.SplitN(fields[6], "=", 2)[1]
	}

	if len(fields) > 7 {
		if !strings.HasPrefix(fields[7], "CSL=") || len(fields[6]) < len("CSL=")+1 {
			return NewGenericFSDError(SyntaxError, fields[7], "invalid CSL= field")
		}
		pdu.CSL = strings.SplitN(fields[7], "=", 2)[1]
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
