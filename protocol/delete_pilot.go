package protocol

import (
	"fmt"
	"strconv"
	"strings"
)

type DeletePilotPDU struct {
	From string `validate:"required,alphanum,max=7"`
	CID  int    `validate:"required,min=100000,max=9999999"`
}

func (p *DeletePilotPDU) Serialize() string {
	return fmt.Sprintf("#DP%s:%d%s", p.From, p.CID, PacketDelimeter)
}

func ParseDeletePilotPDU(rawPacket string) (*DeletePilotPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "#DP")
	fields := strings.Split(rawPacket, Delimeter)

	if len(fields) != 2 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	cid, err := strconv.Atoi(fields[1])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := DeletePilotPDU{
		From: fields[0],
		CID:  cid,
	}

	err = V.Struct(pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
