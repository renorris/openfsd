package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
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
			want:    &AuthChallengePDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Missing To Field",
			packet:  "$ZCSERVER::abcd1234ef\r\n",
			want:    &AuthChallengePDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid Challenge Len",
			packet:  "$ZCSERVER:CLIENT:ab\r\n",
			want:    &AuthChallengePDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid Hexadecimal Challenge",
			packet:  "$ZCSERVER:CLIENT:ghij7890\r\n",
			want:    &AuthChallengePDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	V = validator.New()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pdu := AuthChallengePDU{}
			err := pdu.Parse(tc.packet)
			assert.Equal(t, tc.want, &pdu)
			// Check the error
			if tc.wantErr != nil {
				if strings.Contains(tc.wantErr.Error(), "validation error") {
					assert.Contains(t, err.Error(), "validation error")
				} else {
					assert.EqualError(t, err, tc.wantErr.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
