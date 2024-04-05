package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
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
		wantErr bool
	}{
		{
			name:   "Valid packet with DoSendFast true",
			packet: "$SFSERVER:CLIENT:1\r\n",
			want: &SendFastPDU{
				From:       "SERVER",
				To:         "CLIENT",
				DoSendFast: true,
			},
			wantErr: false,
		},
		{
			name:   "Valid packet with DoSendFast false",
			packet: "$SFSERVER:CLIENT:0\r\n",
			want: &SendFastPDU{
				From:       "SERVER",
				To:         "CLIENT",
				DoSendFast: false,
			},
			wantErr: false,
		},
		{
			name:    "Packet with missing DoSendFast field",
			packet:  "$SFSERVER:CLIENT:\r\n",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Packet with invalid DoSendFast field",
			packet:  "$SFSERVER:CLIENT:not_a_number\r\n",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Incorrect packet format",
			packet:  "SFSERVERCLIENT0\r\n",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParseSendFastPDU(tc.packet)

			// Check the error
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			assert.Equal(t, tc.want, result)
		})
	}
}
