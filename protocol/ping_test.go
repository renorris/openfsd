package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPingPDU_Serialize(t *testing.T) {
	tests := []struct {
		name     string
		pdu      PingPDU
		expected string
	}{
		{
			name: "Valid PingPDU",
			pdu: PingPDU{
				From:      "SOURCE",
				To:        "TARGET",
				Timestamp: "1609459200",
			},
			expected: "$PISOURCE:TARGET:1609459200\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.pdu.Serialize())
		})
	}
}

func TestParsePingPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *PingPDU
		wantErr error
	}{
		{
			name:   "Valid PingPDU Parse",
			packet: "$PISOURCE:TARGET:1609459200\r\n",
			want: &PingPDU{
				From:      "SOURCE",
				To:        "TARGET",
				Timestamp: "1609459200",
			},
			wantErr: nil,
		},
		{
			name:    "Missing fields",
			packet:  "$PISOURCE:1609459200\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Exceeds max field length",
			packet:  "$PISOURCESOURCE:TARGETTARGET:16094592000000000000000000000000\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid From field",
			packet:  "$PI12345678:TARGET:1609459200\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
		{
			name:    "Invalid To field",
			packet:  "$PISOURCE:12345678:1609459200\r\n",
			want:    nil,
			wantErr: NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParsePingPDU(tc.packet)

			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tc.want, result)
		})
	}
}
