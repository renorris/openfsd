package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type ClientIdentificationPDU struct {
	From             string `validate:"required,alphanum,max=7"`
	To               string `validate:"required,alphanum,max=7"`
	ClientID         uint16 `validate:"required"`
	ClientName       string `validate:"required,alphanum,max=16"`
	MajorVersion     int    `validate:""`
	MinorVersion     int    `validate:""`
	CID              int    `validate:"required,min=100000,max=9999999"`
	SysUID           int    `validate:"required,number"`
	InitialChallenge string `validate:"required,hexadecimal,min=2,max=32"`
}

func (p *ClientIdentificationPDU) Serialize() string {
	clientIDBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(clientIDBytes, p.ClientID)
	clientIDStr := hex.EncodeToString(clientIDBytes)
	return fmt.Sprintf("$ID%s:%s:%s:%s:%d:%d:%d:%d:%s%s", p.From, p.To, clientIDStr, p.ClientName, p.MajorVersion, p.MinorVersion, p.CID, p.SysUID, p.InitialChallenge, PacketDelimeter)
}

func ParseClientIdentificationPDU(rawPacket string) (*ClientIdentificationPDU, error) {
	rawPacket = strings.TrimSuffix(rawPacket, PacketDelimeter)
	rawPacket = strings.TrimPrefix(rawPacket, "$ID")
	fields := strings.Split(rawPacket, Delimeter)
	if len(fields) != 9 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	// fields[2] == uint16 in hexadecimal
	if len(fields[2]) != 4 {
		return nil, NewGenericFSDError(SyntaxError)
	}

	clientIDBytes, err := hex.DecodeString(fields[2])
	if err != nil || len(clientIDBytes) != 2 {
		return nil, NewGenericFSDError(SyntaxError)
	}
	clientID := binary.BigEndian.Uint16(clientIDBytes)

	majorVersion, err := strconv.Atoi(fields[4])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	minorVersion, err := strconv.Atoi(fields[5])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	cid, err := strconv.Atoi(fields[6])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	sysUID, err := strconv.Atoi(fields[7])
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	pdu := ClientIdentificationPDU{
		From:             fields[0],
		To:               fields[1],
		ClientID:         clientID,
		ClientName:       fields[3],
		MajorVersion:     majorVersion,
		MinorVersion:     minorVersion,
		CID:              cid,
		SysUID:           sysUID,
		InitialChallenge: fields[8],
	}

	err = V.Struct(&pdu)
	if err != nil {
		return nil, NewGenericFSDError(SyntaxError)
	}

	return &pdu, nil
}
