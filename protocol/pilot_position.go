package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type PilotPositionPDU struct {
	SquawkingModeC   bool    `validate:""`
	Identing         bool    `validate:""`
	From             string  `validate:"required,alphanum,max=16"`
	SquawkCode       string  `validate:"len=4"`
	NetworkRating    int     `validate:"required,min=1,max=12"`
	Lat              float64 `validate:"min=-90.0,max=90.0"`
	Lng              float64 `validate:"min=-180.0,max=180.0"`
	TrueAltitude     int     `validate:"min=-1500,max=99999"`
	PressureAltitude int     `validate:"min=-1500,max=99999"`
	GroundSpeed      int     `validate:"min=0,max=9999"`
	Pitch            float64 `validate:"min=-360.0,max=360.0"`
	Heading          float64 `validate:"min=-360.0,max=360.0"`
	Bank             float64 `validate:"min=-360.0,max=360.0"`
}

func packPitchBankHeading(pitch, bank, heading float64) uint32 {
	p := pitch / -360.0
	if p < 0 {
		p += 1.0
	}
	p *= 1024.0

	b := bank / -360.0
	if b < 0 {
		b += 1.0
	}
	b *= 1024.0

	h := heading / 360.0 * 1024.0

	return uint32(p)<<22 | uint32(b)<<12 | uint32(h)<<2
}

func unpackPitchBankHeading(pbh uint32) (pitch, bank, heading float64) {
	pitchInt := pbh >> 22
	pitch = float64(pitchInt) / 1024.0 * -360.0
	if pitch > 180.0 {
		pitch -= 360.0
	} else if pitch <= -180.0 {
		pitch += 360.0
	}

	bankInt := (pbh >> 12) & 0x3FF
	bank = float64(bankInt) / 1024.0 * -360.0
	if bank > 180.0 {
		bank -= 360.0
	} else if bank <= -180.0 {
		bank += 360.0
	}

	headingInt := (pbh >> 2) & 0x3FF
	heading = float64(headingInt) / 1024.0 * 360.0
	if heading < 0.0 {
		heading += 360.0
	} else if heading >= 360.0 {
		heading -= 360.0
	}

	return
}

func (p *PilotPositionPDU) Serialize() string {
	xpdrStateStr := "S"
	if p.Identing {
		xpdrStateStr = "Y"
	} else if p.SquawkingModeC {
		xpdrStateStr = "N"
	}

	return fmt.Sprintf("@%s:%s:%s:%d:%.6f:%.6f:%d:%d:%d:%d%s", xpdrStateStr, p.From, p.SquawkCode, p.NetworkRating, p.Lat, p.Lng, p.TrueAltitude, p.GroundSpeed, packPitchBankHeading(p.Pitch, p.Bank, p.Heading), p.PressureAltitude-p.TrueAltitude, PacketDelimiter)
}

func (p *PilotPositionPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "@")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 10 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := PilotPositionPDU{
		From:       fields[1],
		SquawkCode: fields[2],
	}

	var err error

	// Parse numeric fields
	if pdu.NetworkRating, err = strconv.Atoi(fields[3]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[3], "invalid network rating")
	}

	if pdu.Lat, err = strconv.ParseFloat(fields[4], 64); err != nil {
		return NewGenericFSDError(SyntaxError, fields[4], "invalid latitude")
	}

	if pdu.Lng, err = strconv.ParseFloat(fields[5], 64); err != nil {
		return NewGenericFSDError(SyntaxError, fields[5], "invalid longitude")
	}

	if pdu.TrueAltitude, err = strconv.Atoi(fields[6]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[6], "invalid true altitude")
	}

	if pdu.GroundSpeed, err = strconv.Atoi(fields[7]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[7], "invalid groundspeed")
	}

	if pdu.PressureAltitude, err = strconv.Atoi(fields[9]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[9], "invalid pressure altitude")
	}

	pdu.PressureAltitude += pdu.TrueAltitude

	// Parse pitch/bank/heading
	var pbh64 uint64
	if pbh64, err = strconv.ParseUint(fields[8], 10, 32); err != nil {
		return NewGenericFSDError(SyntaxError, fields[8], "invalid pitch/bank/heading integer")
	}
	pdu.Pitch, pdu.Bank, pdu.Heading = unpackPitchBankHeading(uint32(pbh64))

	switch fields[0] {
	case "N":
		pdu.SquawkingModeC = true
	case "Y":
		pdu.SquawkingModeC = true
		pdu.Identing = true
	case "S":
	default:
		return NewGenericFSDError(SyntaxError, fields[0], "invalid transponder state identifier")
	}

	// Validate
	if err = V.Struct(pdu); err != nil {
		if validatorErr := getFSDErrorFromValidatorErrors(err); err != nil {
			return validatorErr
		}
		return err
	}

	// Check transponder code validity
	for _, char := range pdu.SquawkCode {
		if char < '0' || char > '7' {
			return NewGenericFSDError(SyntaxError, fields[2], "invalid transponder code")
		}
	}

	// Copy new pdu into receiver
	*p = pdu

	return nil
}
