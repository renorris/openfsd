package protocol

import (
	"fmt"
	"strings"
)

type PlaneInfoRequestFsinnPDU struct {
	From                     string `validate:"required,alphanum,max=7"`
	To                       string `validate:"required,alphanum,max=7"`
	AirlineICAO              string `validate:"alphanum,max=4"`
	AircraftICAO             string `validate:"alphanum,max=4"`
	AircraftICAOCombinedType string `validate:"alphanum,max=4"`
	SendMModel               string `validate:"max=128"`
}

func (p *PlaneInfoRequestFsinnPDU) Serialize() string {
	return fmt.Sprintf("#SB%s:%s:FSIPIR:0:%s:%s:::::%s:%s%s", p.From, p.To, p.AirlineICAO, p.AircraftICAO, p.AircraftICAOCombinedType, p.SendMModel, PacketDelimeter)
}

func ParsePlaneInfoRequestFsinnPDU(rawPacket string) (*PlaneInfoRequestFsinnPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "#SB")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) != 12 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	if fields[2] != "FSIPIR" {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := &PlaneInfoRequestFsinnPDU{
		From:                     fields[0],
		To:                       fields[1],
		AirlineICAO:              fields[4],
		AircraftICAO:             fields[5],
		AircraftICAOCombinedType: fields[10],
		SendMModel:               fields[11],
	}

	err := V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return pdu, nil
}
