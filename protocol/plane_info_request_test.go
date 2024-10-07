package protocol

import (
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

func TestParsePlaneInfoRequestPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *PlaneInfoRequestPDU
		wantErr error
	}{
		{
			name:   "Valid input",
			packet: "#SBPILOT:ATC:PIR\r\n",
			want: &PlaneInfoRequestPDU{
				From: "PILOT",
				To:   "ATC",
			},
			wantErr: nil,
		},
		{
			name:    "Last element not PIR",
			packet:  "#SBPILOT:ATC:PI\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "PI", "third parameter must be 'PIR'"),
		},
		{
			name:    "Missing To field",
			packet:  "#SBPILOT::PIR\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Missing From field",
			packet:  "#SB:ATC:PIR\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Extra fields",
			packet:  "#SBPILOT:ATC:EXTRA:PIR\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			name:    "Invalid From field (non-alphanumerical)",
			packet:  "#SBP!@#$:ATC:PIR\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid To field (too long)",
			packet:  "#SBPILOT:1234567812345678123456781234567812345678:PIR\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Input with incorrect prefix",
			packet:  "$DIPILOT:ATC:PIR\r\n",
			want:    &PlaneInfoRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := PlaneInfoRequestPDU{}
			err := pdu.Parse(tc.packet)

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

			// Verify the result
			assert.Equal(t, tc.want, &pdu)
		})
	}
}

func TestPlaneInfoRequestPDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  PlaneInfoRequestPDU
		want string
	}{
		{
			name: "Valid serialization",
			pdu: PlaneInfoRequestPDU{
				From: "PILOT",
				To:   "ATC",
			},
			want: "#SBPILOT:ATC:PIR\r\n", // assuming correct values for constants
		},
		{
			name: "Minimal length fields",
			pdu: PlaneInfoRequestPDU{
				From: "A",
				To:   "B",
			},
			want: "#SBA:B:PIR\r\n", // assuming correct values for constants
		},
		{
			name: "Max length fields",
			pdu: PlaneInfoRequestPDU{
				From: "1234567",
				To:   "7654321",
			},
			want: "#SB1234567:7654321:PIR\r\n", // assuming correct values for constants
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform serialization
			result := tc.pdu.Serialize()

			// Verify the result
			assert.Equal(t, tc.want, result)
		})
	}
}
