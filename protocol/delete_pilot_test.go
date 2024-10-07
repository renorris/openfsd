package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParseDeletePilotPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *DeletePilotPDU
		wantErr error
	}{
		{
			"Valid Packet",
			"#DPCTRLLL:1234567\r\n",
			&DeletePilotPDU{
				From: "CTRLLL",
				CID:  1234567,
			},
			nil,
		},
		{
			"Invalid From Field (Too Long)",
			"#DPCONTROLLER1CONTROLLER1CONTROLLER1CONTROLLER1:1234567\r\n",
			&DeletePilotPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid CID Field (Non-Numeric)",
			"#DPCTRLLL:ABCDEF1\r\n",
			&DeletePilotPDU{},
			NewGenericFSDError(SyntaxError, "ABCDEF1", "invalid CID"),
		},
		{
			"Invalid CID Field (Wrong Len)",
			"#DPCTRLLL:12345\r\n",
			&DeletePilotPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Extra Fields",
			"#DPCTRLLL:1234567:ExtraData\r\n",
			&DeletePilotPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Missing CID",
			"#DPCTRLLL:\r\n",
			&DeletePilotPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid CID"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := DeletePilotPDU{}
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

func TestDeletePilotPDU_Serialize(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name string
		pdu  DeletePilotPDU
		want string
	}{
		{
			name: "Valid Serialization",
			pdu: DeletePilotPDU{
				From: "N123",
				CID:  7654321,
			},
			want: "#DPN123:7654321\r\n",
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
