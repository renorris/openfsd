package protocol

import (
	"errors"
	"fmt"
	"strings"
)

type PlaneInfoPDU struct {
	From      string `validate:"required,alphanum,max=16"`
	To        string `validate:"required,alphanum,max=16"`
	Equipment string `validate:"max=64"`
	Airline   string `validate:"max=64"`
	Livery    string `validate:"max=64"`
	CSL       string `validate:"max=64"`
}

var NoKeyFoundError = noKeyFoundError()

func noKeyFoundError() error {
	return errors.New("plane info response: no key found")
}

// findPlaneInfoValue attempts to find a given key `key` in fields `fields`
// formatted as e.g. KEY=VALUE.
func findPlaneInfoValue(key string, fields []string) (val string, err error) {
	for _, field := range fields {
		if !strings.HasPrefix(field, key) {
			continue
		}

		var s []string
		if s = strings.Split(field, "="); len(s) != 2 {
			continue
		}

		val = s[1]
		return
	}

	err = NoKeyFoundError
	return
}

func (p *PlaneInfoPDU) Serialize() string {
	str := fmt.Sprintf("#SB%s:%s:PI:GEN", p.From, p.To)
	if p.Equipment != "" {
		str += fmt.Sprintf(":EQUIPMENT=%s", p.Equipment)
	}
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

func (p *PlaneInfoPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "#SB")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) < 5 || len(fields) > 8 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	if fields[2] != "PI" {
		return NewGenericFSDError(SyntaxError, fields[2], "third parameter must be 'PI'")
	}

	if fields[3] != "GEN" {
		return NewGenericFSDError(SyntaxError, fields[3], "fourth parameter must be 'GEN'")
	}

	pdu := PlaneInfoPDU{
		From: fields[0],
		To:   fields[1],
	}

	// Determine number of optional fields to parse
	numOptionalFields := len(fields) - 4

	// Store each optional field
	for range numOptionalFields {
		optionalFields := fields[4:]
		if val, err := findPlaneInfoValue("EQUIPMENT", optionalFields); err == nil {
			pdu.Equipment = val
		}
		if val, err := findPlaneInfoValue("AIRLINE", optionalFields); err == nil {
			pdu.Airline = val
		}
		if val, err := findPlaneInfoValue("LIVERY", optionalFields); err == nil {
			pdu.Livery = val
		}
		if val, err := findPlaneInfoValue("CSL", optionalFields); err == nil {
			pdu.CSL = val
		}
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
