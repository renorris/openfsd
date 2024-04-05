package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAuthChallengeResponsePDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  *AuthChallengeResponsePDU
		want string
	}{
		{
			name: "Valid Serialization",
			pdu: &AuthChallengeResponsePDU{
				From:              "SERVER",
				To:                "CLIENT",
				ChallengeResponse: "0c4a96fa1cab961018620f120988cdf9",
			},
			want: "$ZRSERVER:CLIENT:0c4a96fa1cab961018620f120988cdf9\r\n",
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

func TestParseAuthChallengeResponsePDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *AuthChallengeResponsePDU
		wantErr error
	}{
		{
			name:   "Valid Packet",
			packet: "$ZRSERVER:CLIENT:0c4a96fa1cab961018620f120988cdf9\r\n",
			want: &AuthChallengeResponsePDU{
				From:              "SERVER",
				To:                "CLIENT",
				ChallengeResponse: "0c4a96fa1cab961018620f120988cdf9",
			},
			wantErr: nil,
		},
		{
			name:    "Missing From Field",
			packet:  "$ZR:CLIENT:0c4a96fa1cab961018620f120988cdf9\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Missing To Field",
			packet:  "$ZRSERVER::0c4a96fa1cab961018620f120988cdf9\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Challenge response not md5",
			packet:  "$ZRSERVER:CLIENT:0c4a96fa1cab9610\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid Hexadecimal",
			packet:  "$ZRSERVER:CLIENT:ghij7890\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
	}

	V = validator.New()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseAuthChallengeResponsePDU(tc.packet)
			assert.Equal(t, tc.want, result)
			assert.Equal(t, tc.wantErr, err)
		})
	}
}
