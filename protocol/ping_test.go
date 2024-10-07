package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
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
			want:    &PingPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			name:    "Exceeds max field length",
			packet:  "$PISOURCESOURCE:TARGETTARGET:" + strings.Repeat("METAR", 1024) + "\r\n",
			want:    &PingPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid From field",
			packet:  "$PI12345678123456781234567812345678:TARGET:1609459200\r\n",
			want:    &PingPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid To field",
			packet:  "$PISOURCE:1234567812345678123456781234567812345678:1609459200\r\n",
			want:    &PingPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pdu := PingPDU{}
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
