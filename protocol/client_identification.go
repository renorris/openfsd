package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type ClientIdentificationPDU struct {
	From             string `validate:"required,alphanum,max=16"`
	To               string `validate:"required,alphanum,max=16"`
	ClientID         uint16 `validate:"required"`
	ClientName       string `validate:"required,max=32"`
	MajorVersion     int    `validate:"min=0,max=999"`
	MinorVersion     int    `validate:"min=0,max=999"`
	CID              int    `validate:"min=100000,max=9999999"`
	SysUID           string `validate:"required,min=1,max=64"`
	InitialChallenge string `validate:"required,hexadecimal,min=2,max=32"`
}

func (p *ClientIdentificationPDU) Serialize() string {
	clientIDBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(clientIDBytes, p.ClientID)
	clientIDStr := hex.EncodeToString(clientIDBytes)
	return fmt.Sprintf("$ID%s:%s:%s:%s:%d:%d:%d:%s:%s%s",
		p.From, p.To, clientIDStr, p.ClientName, p.MajorVersion,
		p.MinorVersion, p.CID, p.SysUID, p.InitialChallenge, PacketDelimiter)
}

func (p *ClientIdentificationPDU) Parse(packet string) error {
	packet = strings.TrimSuffix(packet, PacketDelimiter)
	packet = strings.TrimPrefix(packet, "$ID")

	var fields []string
	if fields = strings.Split(packet, Delimiter); len(fields) != 9 {
		return NewGenericFSDError(SyntaxError, "", "invalid parameter count")
	}

	pdu := ClientIdentificationPDU{
		From:             fields[0],
		To:               fields[1],
		ClientName:       fields[3],
		SysUID:           fields[7],
		InitialChallenge: fields[8],
	}

	// fields[2] == uint16 in hexadecimal
	if len(fields[2]) != 4 {
		return NewGenericFSDError(SyntaxError, fields[2],
			"client ID must be 4 hexadecimal characters")
	}

	var clientIDBytes []byte
	var err error
	if clientIDBytes, err = hex.DecodeString(fields[2]); err != nil || len(clientIDBytes) != 2 {
		return NewGenericFSDError(SyntaxError, fields[2], "invalid client ID")
	}
	pdu.ClientID = binary.BigEndian.Uint16(clientIDBytes)

	if pdu.MajorVersion, err = strconv.Atoi(fields[4]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[4], "invalid major version")
	}

	if pdu.MinorVersion, err = strconv.Atoi(fields[5]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[5], "invalid minor version")
	}

	if pdu.CID, err = strconv.Atoi(fields[6]); err != nil {
		return NewGenericFSDError(SyntaxError, fields[6], "invalid CID")
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
