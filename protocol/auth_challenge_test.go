package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAuthChallengePDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  *AuthChallengePDU
		want string
	}{
		{
			name: "Valid Serialization",
			pdu: &AuthChallengePDU{
				From:      "SERVER",
				To:        "CLIENT",
				Challenge: "abcd1234ef",
			},
			want: "$ZCSERVER:CLIENT:abcd1234ef\r\n",
		},
	}

	V = validator.New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.pdu.Serialize()
			assert.Equal(t, test.want, result)
		})
	}
}

func TestParseAuthChallengePDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *AuthChallengePDU
		wantErr error
	}{
		{
			name:   "Valid Packet",
			packet: "$ZCSERVER:CLIENT:abcd1234ef\r\n",
			want: &AuthChallengePDU{
				From:      "SERVER",
				To:        "CLIENT",
				Challenge: "abcd1234ef",
			},
			wantErr: nil,
		},
		{
			name:    "Missing From Field",
			packet:  "$ZC:CLIENT:abcd1234ef\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Missing To Field",
			packet:  "$ZCSERVER::abcd1234ef\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid Challenge Length",
			packet:  "$ZCSERVER:CLIENT:ab\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid Hexadecimal Challenge",
			packet:  "$ZCSERVER:CLIENT:ghij7890\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
	}

	V = validator.New()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseAuthChallengePDU(tc.packet)
			assert.Equal(t, tc.want, result)
			assert.Equal(t, tc.wantErr, err)
		})
	}
}
