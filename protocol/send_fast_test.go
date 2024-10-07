package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestSendFastPDU_Serialize(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name string
		pdu  SendFastPDU
		want string
	}{
		{
			name: "SendFastPDU with DoSendFast true",
			pdu: SendFastPDU{
				From:       "SERVER",
				To:         "CLIENT",
				DoSendFast: true,
			},
			want: "$SFSERVER:CLIENT:1\r\n",
		},
		{
			name: "SendFastPDU with DoSendFast false",
			pdu: SendFastPDU{
				From:       "SERVER",
				To:         "CLIENT",
				DoSendFast: false,
			},
			want: "$SFSERVER:CLIENT:0\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the serialization
			result := tc.pdu.Serialize()

			// Verify the result
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestParseSendFastPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *SendFastPDU
		wantErr error
	}{
		{
			name:   "Valid packet with DoSendFast true",
			packet: "$SFSERVER:CLIENT:1\r\n",
			want: &SendFastPDU{
				From:       "SERVER",
				To:         "CLIENT",
				DoSendFast: true,
			},
			wantErr: nil,
		},
		{
			name:   "Valid packet with DoSendFast false",
			packet: "$SFSERVER:CLIENT:0\r\n",
			want: &SendFastPDU{
				From:       "SERVER",
				To:         "CLIENT",
				DoSendFast: false,
			},
			wantErr: nil,
		},
		{
			name:    "Packet with missing DoSendFast field",
			packet:  "$SFSERVER:CLIENT:\r\n",
			want:    &SendFastPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "invalid send fast integer"),
		},
		{
			name:    "Packet with invalid DoSendFast field",
			packet:  "$SFSERVER:CLIENT:not_a_number\r\n",
			want:    &SendFastPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "not_a_number", "invalid send fast integer"),
		},
		{
			name:    "out of bounds send fast integer",
			packet:  "$SFSERVER:CLIENT:2\r\n",
			want:    &SendFastPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "2", "send fast integer must be 1 or 0"),
		},
		{
			name:    "Incorrect packet format",
			packet:  "SFSERVERCLIENT0\r\n",
			want:    &SendFastPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := SendFastPDU{}
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

			// Verify the result
			assert.Equal(t, tc.want, &pdu)
		})
	}
}
