package protocol

import (
	"fmt"
	"strings"
)

type PlaneInfoResponsePDU struct {
	From      string `validate:"required,alphanum,max=7"`
	To        string `validate:"required,alphanum,max=7"`
	Equipment string `validate:"required,max=32"`
	Airline   string `validate:"max=32"`
	Livery    string `validate:"max=32"`
	CSL       string `validate:"max=32"`
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

	str += PacketDelimeter
	return str
}

func ParsePlaneInfoResponsePDU(rawPacket string) (*PlaneInfoResponsePDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "#SB")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) < 5 || len(fields) > 8 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	if fields[2] != "PI" || fields[3] != "GEN" {
		return nil, NewGenericFSDError(SyntaxError)
	}

	var equipment, airline, livery, csl string

	if !strings.HasPrefix(fields[4], "EQUIPMENT=") || len(fields[4]) < len("EQUIPMENT=")+1 {
		return nil, NewGenericFSDError(SyntaxError)
	}
	equipment = strings.SplitN(fields[4], "=", 2)[1]

	if len(fields) > 5 {
		if !strings.HasPrefix(fields[5], "AIRLINE=") || len(fields[5]) < len("AIRLINE=")+1 {
			return nil, NewGenericFSDError(SyntaxError)
		}
		airline = strings.SplitN(fields[5], "=", 2)[1]
	}

	if len(fields) > 6 {
		if !strings.HasPrefix(fields[6], "LIVERY=") || len(fields[6]) < len("LIVERY=")+1 {
			return nil, NewGenericFSDError(SyntaxError)
		}
		livery = strings.SplitN(fields[6], "=", 2)[1]
	}

	if len(fields) > 7 {
		if !strings.HasPrefix(fields[7], "CSL=") || len(fields[6]) < len("CSL=")+1 {
			return nil, NewGenericFSDError(SyntaxError)
		}
		csl = strings.SplitN(fields[7], "=", 2)[1]
	}

	pdu := &PlaneInfoResponsePDU{
		From:      fields[0],
		To:        fields[1],
		Equipment: equipment,
		Airline:   airline,
		Livery:    livery,
		CSL:       csl,
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return pdu, nil
}
