package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParseKillRequestPDU(t *testing.T) {
	V = validator.New()

	tests := []struct {
		name    string
		packet  string
		want    *KillRequestPDU
		wantErr error
	}{
		{
			name:   "Valid Reason",
			packet: "$!!JOHN:DOE:you're banned: reason\r\n",
			want: &KillRequestPDU{
				From:   "JOHN",
				To:     "DOE",
				Reason: "you're banned: reason",
			},
			wantErr: nil,
		},
		{
			name:   "Valid with no Reason",
			packet: "$!!JOHN:DOE\r\n",
			want: &KillRequestPDU{
				From:   "JOHN",
				To:     "DOE",
				Reason: "",
			},
			wantErr: nil,
		},
		{
			name:    "Invalid from field",
			packet:  "$!!JOHN99999JOHN99999JOHN99999JOHN99999:DOE:you're banned: reason\r\n",
			want:    &KillRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Invalid to field",
			packet:  "$!!JOHN:DOE1234567DOE1234567DOE1234567DOE1234567:you're banned: reason\r\n",
			want:    &KillRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			name:    "Missing to field",
			packet:  "$!!JOHN::Hello, world!\r\n",
			want:    &KillRequestPDU{},
			wantErr: NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := KillRequestPDU{}
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

func TestKillRequestPDU_Serialize(t *testing.T) {
	tests := []struct {
		name       string
		textPDU    KillRequestPDU
		wantOutput string
	}{
		{
			name: "Valid",
			textPDU: KillRequestPDU{
				From:   "ALPHA1",
				To:     "BRAVO2",
				Reason: "Hello, this is a test Reason.",
			},
			wantOutput: "$!!ALPHA1:BRAVO2:Hello, this is a test Reason.\r\n",
		},
		{
			name: "Valid without Reason",
			textPDU: KillRequestPDU{
				From:   "ALPHA1",
				To:     "BRAVO2",
				Reason: "",
			},
			wantOutput: "$!!ALPHA1:BRAVO2\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform serialization
			output := tc.textPDU.Serialize()

			// Verify the output
			assert.Equal(t, tc.wantOutput, output)
		})
	}
}
