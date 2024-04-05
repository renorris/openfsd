package protocol

import (
	"encoding/hex"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
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
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid Major Version",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:X:2:0001234:12345678:abcd1234\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid ClientID",
			packet:  "$IDCLIENT:SERVER:12:ClientName:1:2:0001234:12345678:abcd1234\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid Hexadecimal in Initial Challenge",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:12345678:xyz\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Initial Challenge Too Short",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:12345678:a\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid Minor Version",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:XY:0001234:12345678:abcd1234\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid SysUID - Non-numeric",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:SYSUID:abcd1234\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid CID - too long",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234567890:12345678:abcd1234\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid CID - contains letters",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:000ABCD:12345678:abcd1234\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Initial Challenge Too Long",
			packet:  "$IDCLIENT:SERVER:1234:ClientName:1:2:0001234:12345678:" + hex.EncodeToString(make([]byte, 33)) + "\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseClientIdentificationPDU(tc.packet)

			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tc.want, result)
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
