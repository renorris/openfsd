package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	FastPilotPositionTypeFast = iota
	FastPilotPositionTypeSlow
	FastPilotPositionTypeStopped
)

type VelocityVector struct {
	X float64 `validate:"min=-9999.0,max=9999.0"`
	Y float64 `validate:"min=-9999.0,max=9999.0"`
	Z float64 `validate:"min=-9999.0,max=9999.0"`
}

type FastPilotPositionPDU struct {
	Type                     int            `validate:"min=0,max=2"`
	From                     string         `validate:"required,alphanum,max=16"`
	Lat                      float64        `validate:"min=-90.0,max=90.0"`
	Lng                      float64        `validate:"min=-180.0,max=180.0"`
	AltitudeTrue             float64        `validate:"min=-1500.0,max=99999.0"`
	AltitudeAgl              float64        `validate:"min=-1500.0,max=99999.0"`
	Pitch                    float64        `validate:"min=-360.0,max=360.0"`
	Heading                  float64        `validate:"min=-360.0,max=360.0"`
	Bank                     float64        `validate:"min=-360.0,max=360.0"`
	PositionalVelocityVector VelocityVector `validate:""` // meters per second
	RotationalVelocityVector VelocityVector `validate:""` // radians per second
	NoseGearAngle            float64        `validate:"min=-360.0,max=360.0"`
}

func (p *FastPilotPositionPDU) Serialize() string {
	switch p.Type {
	case FastPilotPositionTypeFast:
		return fmt.Sprintf("^%s:%.6f:%.6f:%.2f:%.2f:%d:%.4f:%.4f:%.4f:%.4f:%.4f:%.4f:%.2f%s",
			p.From, p.Lat, p.Lng, p.AltitudeTrue,
			p.AltitudeAgl, packPitchBankHeading(p.Pitch, p.Bank, p.Heading),
			p.PositionalVelocityVector.X, p.PositionalVelocityVector.Y,
			p.PositionalVelocityVector.Z, p.RotationalVelocityVector.X,
			p.RotationalVelocityVector.Y, p.RotationalVelocityVector.Z,
			p.NoseGearAngle, PacketDelimiter)

	case FastPilotPositionTypeSlow:
		return fmt.Sprintf("#SL%s:%.6f:%.6f:%.2f:%.2f:%d:%.4f:%.4f:%.4f:%.4f:%.4f:%.4f:%.2f%s",
			p.From, p.Lat, p.Lng, p.AltitudeTrue,
			p.AltitudeAgl, packPitchBankHeading(p.Pitch, p.Bank, p.Heading),
			p.PositionalVelocityVector.X, p.PositionalVelocityVector.Y,
			p.PositionalVelocityVector.Z, p.RotationalVelocityVector.X,
			p.RotationalVelocityVector.Y, p.RotationalVelocityVector.Z,
			p.NoseGearAngle, PacketDelimiter)

	default: // FastPilotPositionTypeStopped
		return fmt.Sprintf("#ST%s:%.6f:%.6f:%.2f:%.2f:%d:%.2f%s",
			p.From, p.Lat, p.Lng, p.AltitudeTrue, p.AltitudeAgl,
			packPitchBankHeading(p.Pitch, p.Bank, p.Heading),
			p.NoseGearAngle, PacketDelimiter)
	}
}

func (p *FastPilotPositionPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)

	// Determine type
	var pduType int
	if strings.HasPrefix(packet, "^") {
		pduType = FastPilotPositionTypeFast
		packet = strings.TrimPrefix(packet, "^")
	} else if strings.HasPrefix(packet, "#SL") {
		pduType = FastPilotPositionTypeSlow
		packet = strings.TrimPrefix(packet, "#SL")
	} else if strings.HasPrefix(packet, "#ST") {
		pduType = FastPilotPositionTypeStopped
		packet = strings.TrimPrefix(packet, "#ST")
	} else {
		return NewGenericFSDError(SyntaxError, "", "invalid packet prefix")
	}

	fields := strings.Split(packet, Delimiter)
	if pduType == FastPilotPositionTypeStopped {
		if len(fields) != 7 {
			return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
		}
	} else {
		if len(fields) != 13 {
			return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
		}
	}

	pdu := FastPilotPositionPDU{
		Type: pduType,
		From: fields[0],
	}

	var err error

	// Parse numeric fields
	if pdu.Lat, err = strconv.ParseFloat(fields[1], 64); err != nil {
		return NewGenericFSDError(SyntaxError, fields[1], "invalid latitude")
	}

	if pdu.Lng, err = strconv.ParseFloat(fields[2], 64); err != nil {
		return NewGenericFSDError(SyntaxError, fields[2], "invalid longitude")
	}

	if pdu.AltitudeTrue, err = strconv.ParseFloat(fields[3], 64); err != nil {
		return NewGenericFSDError(SyntaxError, fields[3], "invalid true altitude")
	}

	if pdu.AltitudeAgl, err = strconv.ParseFloat(fields[4], 64); err != nil {
		return NewGenericFSDError(SyntaxError, fields[4], "invalid above-ground-level altitude")
	}

	// Parse pitch/bank/heading
	var pbh uint64
	if pbh, err = strconv.ParseUint(fields[5], 10, 32); err != nil {
		return NewGenericFSDError(SyntaxError, fields[5], "invalid pitch/bank/heading integer")
	}
	pdu.Pitch, pdu.Bank, pdu.Heading = unpackPitchBankHeading(uint32(pbh))

	if pduType == FastPilotPositionTypeStopped {
		// Set zero values for velocity and rotational vectors
		pdu.PositionalVelocityVector = VelocityVector{}
		pdu.RotationalVelocityVector = VelocityVector{}
		if pdu.NoseGearAngle, err = strconv.ParseFloat(fields[6], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[6], "invalid nose gear angle")
		}
	} else {
		// Parse values
		if pdu.PositionalVelocityVector.X, err = strconv.ParseFloat(fields[6], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[6], "invalid positional X velocity vector")
		}
		if pdu.PositionalVelocityVector.Y, err = strconv.ParseFloat(fields[7], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[7], "invalid positional Y velocity vector")
		}
		if pdu.PositionalVelocityVector.Z, err = strconv.ParseFloat(fields[8], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[8], "invalid positional Z velocity vector")
		}
		if pdu.RotationalVelocityVector.X, err = strconv.ParseFloat(fields[9], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[9], "invalid rotational X velocity vector")
		}
		if pdu.RotationalVelocityVector.Y, err = strconv.ParseFloat(fields[10], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[10], "invalid rotational Y velocity vector")
		}
		if pdu.RotationalVelocityVector.Z, err = strconv.ParseFloat(fields[11], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[11], "invalid rotational Z velocity vector")
		}
		if pdu.NoseGearAngle, err = strconv.ParseFloat(fields[12], 64); err != nil {
			return NewGenericFSDError(SyntaxError, fields[12], "invalid nose gear angle")
		}
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
