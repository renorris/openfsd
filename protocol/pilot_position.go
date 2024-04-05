package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type PilotPositionPDU struct {
	SquawkingModeC   bool    `validate:""`
	Identing         bool    `validate:""`
	From             string  `validate:"required,alphanum,max=7"`
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

	return fmt.Sprintf("@%s:%s:%s:%d:%.6f:%.6f:%d:%d:%d:%d%s", xpdrStateStr, p.From, p.SquawkCode, p.NetworkRating, p.Lat, p.Lng, p.TrueAltitude, p.GroundSpeed, packPitchBankHeading(p.Pitch, p.Bank, p.Heading), p.PressureAltitude-p.TrueAltitude, PacketDelimeter)
}

func ParsePilotPositionPDU(rawPacket string) (*PilotPositionPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "@")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) != 10 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	networkRating, err := strconv.Atoi(fields[3])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	lat, err := strconv.ParseFloat(fields[4], 64)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	lng, err := strconv.ParseFloat(fields[5], 64)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	trueAlt, err := strconv.Atoi(fields[6])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	groundspeed, err := strconv.Atoi(fields[7])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pbh64, err := strconv.ParseUint(fields[8], 10, 32)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}
	pbh := uint32(pbh64)

	pitch, bank, heading := unpackPitchBankHeading(pbh)

	pressureAlt, err := strconv.Atoi(fields[9])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}
	pressureAlt += trueAlt

	identing := false
	modeC := false
	if fields[0] == "N" {
		modeC = true
	} else if fields[0] == "Y" {
		modeC = true
		identing = true
	} else if fields[0] != "S" {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := PilotPositionPDU{
		SquawkingModeC:   modeC,
		Identing:         identing,
		From:             fields[1],
		SquawkCode:       fields[2],
		NetworkRating:    networkRating,
		Lat:              lat,
		Lng:              lng,
		TrueAltitude:     trueAlt,
		PressureAltitude: pressureAlt,
		GroundSpeed:      groundspeed,
		Pitch:            pitch,
		Heading:          bank,
		Bank:             heading,
	}

	err = V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	// Check transponder code validity
	for _, char := range pdu.SquawkCode {
		if char < '0' || char > '7' {
			return nil, NewGenericFSDError(SyntaxError)
		}
	}

	return &pdu, nil
}
