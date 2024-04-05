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
	From                     string         `validate:"required,alphanum,max=7"`
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
		return fmt.Sprintf("^%s:%.6f:%.6f:%.2f:%.2f:%d:%.4f:%.4f:%.4f:%.4f:%.4f:%.4f:%.2f%s", p.From, p.Lat, p.Lng, p.AltitudeTrue, p.AltitudeAgl, packPitchBankHeading(p.Pitch, p.Bank, p.Heading), p.PositionalVelocityVector.X, p.PositionalVelocityVector.Y, p.PositionalVelocityVector.Z, p.RotationalVelocityVector.X, p.RotationalVelocityVector.Y, p.RotationalVelocityVector.Z, p.NoseGearAngle, PacketDelimeter)
	case FastPilotPositionTypeSlow:
		return fmt.Sprintf("#SL%s:%.6f:%.6f:%.2f:%.2f:%d:%.4f:%.4f:%.4f:%.4f:%.4f:%.4f:%.2f%s", p.From, p.Lat, p.Lng, p.AltitudeTrue, p.AltitudeAgl, packPitchBankHeading(p.Pitch, p.Bank, p.Heading), p.PositionalVelocityVector.X, p.PositionalVelocityVector.Y, p.PositionalVelocityVector.Z, p.RotationalVelocityVector.X, p.RotationalVelocityVector.Y, p.RotationalVelocityVector.Z, p.NoseGearAngle, PacketDelimeter)
	default: // FastPilotPositionTypeStopped
		return fmt.Sprintf("#ST%s:%.6f:%.6f:%.2f:%.2f:%d:%.2f%s", p.From, p.Lat, p.Lng, p.AltitudeTrue, p.AltitudeAgl, packPitchBankHeading(p.Pitch, p.Bank, p.Heading), p.NoseGearAngle, PacketDelimeter)
	}
}

func ParseFastPilotPositionPDU(rawPacket string) (*FastPilotPositionPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)

	var pduType int
	if strings.HasPrefix(rawPacket, "^") {
		pduType = FastPilotPositionTypeFast
		rawPacket = strings.TrimPrefix(rawPacket, "^")
	} else if strings.HasPrefix(rawPacket, "#SL") {
		pduType = FastPilotPositionTypeSlow
		rawPacket = strings.TrimPrefix(rawPacket, "#SL")
	} else if strings.HasPrefix(rawPacket, "#ST") {
		pduType = FastPilotPositionTypeStopped
		rawPacket = strings.TrimPrefix(rawPacket, "#ST")
	} else {
		return nil, NewGenericFSDError(SyntaxError)
	}

	fields := strings.Split(rawPacket, Delimeter)

	if pduType == FastPilotPositionTypeStopped {
		if len(fields) != 7 {
			return nil, NewGenericFSDError(SyntaxError)
		}
	} else {
		if len(fields) != 13 {
			return nil, NewGenericFSDError(SyntaxError)
		}
	}

	lat, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	lng, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	altTrue, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	altAgl, err := strconv.ParseFloat(fields[4], 64)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pbh, err := strconv.ParseUint(fields[5], 10, 32)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}
	pitch, bank, heading := unpackPitchBankHeading(uint32(pbh))

	var positionalVector VelocityVector
	var rotationalVector VelocityVector
	var noseGearAngle float64
	if pduType == FastPilotPositionTypeStopped {
		positionalVector = VelocityVector{
			X: 0,
			Y: 0,
			Z: 0,
		}
		rotationalVector = VelocityVector{
			X: 0,
			Y: 0,
			Z: 0,
		}
		noseGearAngle, err = strconv.ParseFloat(fields[6], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}
	} else {
		positionalVector.X, err = strconv.ParseFloat(fields[6], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}

		positionalVector.Y, err = strconv.ParseFloat(fields[7], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}

		positionalVector.Z, err = strconv.ParseFloat(fields[8], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}

		rotationalVector.X, err = strconv.ParseFloat(fields[9], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}

		rotationalVector.Y, err = strconv.ParseFloat(fields[10], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}

		rotationalVector.Z, err = strconv.ParseFloat(fields[11], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}

		noseGearAngle, err = strconv.ParseFloat(fields[12], 64)
		if err != nil {
			return nil, NewGenericFSDError(SyntaxError)
		}
	}

	pdu := FastPilotPositionPDU{
		Type:                     pduType,
		From:                     fields[0],
		Lat:                      lat,
		Lng:                      lng,
		AltitudeTrue:             altTrue,
		AltitudeAgl:              altAgl,
		Pitch:                    pitch,
		Heading:                  heading,
		Bank:                     bank,
		PositionalVelocityVector: positionalVector,
		RotationalVelocityVector: rotationalVector,
		NoseGearAngle:            noseGearAngle,
	}

	err = V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
