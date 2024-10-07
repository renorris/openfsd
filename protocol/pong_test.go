package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestPongPDU_Serialize(t *testing.T) {
	tests := []struct {
		name     string
		pdu      PongPDU
		expected string
	}{
		{
			name: "Valid PongPDU",
			pdu: PongPDU{
				From:      "SOURCE",
				To:        "TARGET",
				Timestamp: "1609459200",
			},
			expected: "$POSOURCE:TARGET:1609459200\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.pdu.Serialize())
		})
	}
}

func TestParsePongPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *PongPDU
		wantErr error
	}{
		{
			name:   "Valid PongPDU Parse",
			packet: "$POSOURCE:TARGET:1609459200\r\n",
			want: &PongPDU{
				From:      "SOURCE",
				To:        "TARGET",
				Timestamp: "1609459200",
			},
			wantErr: nil,
		},
		{
			name:    "Missing fields",
			packet:  "$POSOURCE:1609459200\r\n",
			want:    &PongPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			name:    "Exceeds max field length",
			packet:  "$POSOURCESOURCESOURCESOURCESOURCESOURCE:TARGETTARGETTARGETTARGETTARGETTARGETTARGETTARGET:16094592000000000000000000000000\r\n",
			want:    &PongPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid From field",
			packet:  "$PO1234567812345678123456781234567812345678:TARGET:1609459200\r\n",
			want:    &PongPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid To field",
			packet:  "$POSOURCE:123456781234567812345678123456781234567812345678:1609459200\r\n",
			want:    &PongPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pdu := PongPDU{}
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
