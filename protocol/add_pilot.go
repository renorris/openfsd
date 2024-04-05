package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type AddPilotPDU struct {
	From             string `validate:"required,alphanum,max=7"`
	To               string `validate:"required,alphanum,max=7"`
	CID              int    `validate:"required,min=100000,max=9999999"`
	Token            string `validate:"required,jwt"`
	NetworkRating    int    `validate:"min=1,max=12"`
	ProtocolRevision int    `validate:""`
	SimulatorType    int    `validate:"min=0,max=6"`
	RealName         string `validate:"required,max=32"`
}

func (p *AddPilotPDU) Serialize() string {
	return fmt.Sprintf("#AP%s:%s:%d:%s:%d:%d:%d:%s%s", p.From, p.To, p.CID, p.Token, p.NetworkRating, p.ProtocolRevision, p.SimulatorType, p.RealName, PacketDelimeter)
}

func ParseAddPilotPDU(rawPacket string) (*AddPilotPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "#AP")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) != 8 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	cid, err := strconv.Atoi(fields[2])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	networkRating, err := strconv.Atoi(fields[4])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	protocolRevision, err := strconv.Atoi(fields[5])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	simulatorType, err := strconv.Atoi(fields[6])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := AddPilotPDU{
		From:             fields[0],
		To:               fields[1],
		CID:              cid,
		Token:            fields[3],
		NetworkRating:    networkRating,
		ProtocolRevision: protocolRevision,
		SimulatorType:    simulatorType,
		RealName:         fields[7],
	}

	err = V.Struct(&pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
