package protocol

import (
	"fmt"
	"strings"
)

type PlaneInfoRequestFsinnPDU struct {
	From                     string `validate:"required,alphanum,max=16"`
	To                       string `validate:"required,alphanum,max=16"`
	AirlineICAO              string `validate:"min=0,max=4"`
	AircraftICAO             string `validate:"min=0,max=4"`
	AircraftICAOCombinedType string `validate:"min=0,max=4"`
	SendMModel               string `validate:"max=256"`
}

func (p *PlaneInfoRequestFsinnPDU) Serialize() string {
	return fmt.Sprintf("#SB%s:%s:FSIPIR:0:%s:%s:::::%s:%s%s", p.From, p.To, p.AirlineICAO, p.AircraftICAO, p.AircraftICAOCombinedType, p.SendMModel, PacketDelimiter)
}

func (p *PlaneInfoRequestFsinnPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#SB")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 12 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	if fields[2] != "FSIPIR" {
		return NewGenericFSDError(SyntaxError, fields[2], "third parameter must be 'FSIPIR'")
	}

	pdu := PlaneInfoRequestFsinnPDU{
		From:                     fields[0],
		To:                       fields[1],
		AirlineICAO:              fields[4],
		AircraftICAO:             fields[5],
		AircraftICAOCombinedType: fields[10],
		SendMModel:               fields[11],
	}

	if err := V.Struct(&pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	// Copy new pdu into receiever
	*p = pdu

	return nil
}
