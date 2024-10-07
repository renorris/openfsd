package protocol

import (
	"encoding/hex"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParseClientIdentificationPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *ClientIdentificationPDU
		wantErr error
	}{
		{
			name:   "Valid",
			packet: "$IDCLIENT:SERVER:1234:ClientName:1:2:1234567:12345678:abcd1234\r\n",
			want: &ClientIdentificationPDU{
				From:             "CLIENT",
				To:               "SERVER",
				ClientID:         0x1234,
				ClientName:       "ClientName",
				MajorVersion:     1,
				MinorVersion:     2,
				CID:              1234567,
				SysUID:           12345678,
				InitialChallenge: "abcd1234",
			},
			wantErr: nil,
		},
		{
			name:    "Missing Field",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:1234567:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			name:    "Invalid Major Version",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:X:2:0001234:12345678:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "X", "invalid major version"),
		},
		{
			name:    "Invalid ClientID",
			packet:  "$IDCLIENT:SERVER:12:ClientName:1:2:0001234:12345678:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "12", "client ID must be 4 hexadecimal characters"),
		},
		{
			name:    "Invalid Hexadecimal in Initial Challenge",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:12345678:xyz\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Initial Challenge Too Short",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:12345678:a\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid Minor Version",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:XY:0001234:12345678:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "XY", "invalid minor version"),
		},
		{
			name:    "Invalid SysUID - Non-numeric",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:SYSUID:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "SYSUID", "invalid system UID"),
		},
		{
			name:    "Invalid CID - too long",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234567890:12345678:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid CID - contains letters",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:000ABCD:12345678:abcd1234\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "000ABCD", "invalid CID"),
		},
		{
			name:    "Initial Challenge Too Long",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:12345678:" + hex.EncodeToString(make([]byte, 33)) + "\r\n",
			want:    &ClientIdentificationPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pdu := ClientIdentificationPDU{}
			err := pdu.Parse(tc.packet)

			if tc.wantErr != nil {
				if strings.Contains(tc.wantErr.Error(), "validation error") {
					assert.Contains(t, err.Error(), "validation error")
				} else {
					assert.EqualError(t, err, tc.wantErr.Error())
				}
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tc.want, &pdu)
		})
	}
}

func TestClientIdentificationPDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  ClientIdentificationPDU
		want string
	}{
		{
			name: "Serialize Valid PDU",
			pdu: ClientIdentificationPDU{
				From:             "CLIENT",
				To:               "SERVER",
				ClientID:         0x1234,
				ClientName:       "ClientName",
				MajorVersion:     1,
				MinorVersion:     2,
				CID:              1234567,
				SysUID:           12345678,
				InitialChallenge: "abcd1234",
			},
			want: "$IDCLIENT:SERVER:1234:ClientName:1:2:1234567:12345678:abcd1234\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.pdu.Serialize()
			assert.Equal(t, tc.want, result)
		})
	}
}
