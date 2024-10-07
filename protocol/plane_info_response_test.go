package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParsePlaneInfoResponsePDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *PlaneInfoResponsePDU
		wantErr error
	}{
		{
			"Valid Info With All Fields",
			"#SBATC:PILOT:PI:GEN:EQUIPMENT=A320:AIRLINE=Delta:LIVERY=Standard:CSL=ModelABC\r\n",
			&PlaneInfoResponsePDU{
				From:      "ATC",
				To:        "PILOT",
				Equipment: "A320",
				Airline:   "Delta",
				Livery:    "Standard",
				CSL:       "ModelABC",
			},
			nil,
		},
		{
			"Valid Info With Minimum Required Fields",
			"#SBATC:PILOT:PI:GEN:EQUIPMENT=A320\r\n",
			&PlaneInfoResponsePDU{
				From:      "ATC",
				To:        "PILOT",
				Equipment: "A320",
				Airline:   "",
				Livery:    "",
				CSL:       "",
			},
			nil,
		},
		{
			"Invalid - Missing EQUIPMENT Prefix",
			"#SBATC:PILOT:PI:GEN:A320:LIVERY=Standard\r\n",
			&PlaneInfoResponsePDU{},
			NewGenericFSDError(SyntaxError, "A320", "invalid EQUIPMENT= field"),
		},
		{
			"Invalid - Wrong HEADER Prefix",
			"$SBATC:PILOT:PI:GEN:EQUIPMENT=A320\r\n",
			&PlaneInfoResponsePDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid - Field Count Less",
			"#SBATC:PILOT:PI:GEN\r\n",
			&PlaneInfoResponsePDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Invalid - Field Count More",
			"#SBATC:PILOT:PI:GEN:EQUIPMENT=A320:AIRLINE=Delta:LIVERY=Standard:CSL=ModelABC:ExtraField\r\n",
			&PlaneInfoResponsePDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Invalid - No From Field",
			"#SB:PILOT:PI:GEN:EQUIPMENT=A320\r\n",
			&PlaneInfoResponsePDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid - No To Field",
			"#SBATC::PI:GEN:EQUIPMENT=A320\r\n",
			&PlaneInfoResponsePDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := PlaneInfoResponsePDU{}
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

func TestPlaneInfoResponsePDU_Serialize(t *testing.T) {
	tests := []struct {
		name    string
		pdu     *PlaneInfoResponsePDU
		wantStr string
	}{
		{
			"All Fields Present",
			&PlaneInfoResponsePDU{
				From:      "ATC",
				To:        "PILOT",
				Equipment: "A320",
				Airline:   "Delta",
				Livery:    "Standard",
				CSL:       "ModelABC",
			},
			"#SBATC:PILOT:PI:GEN:EQUIPMENT=A320:AIRLINE=Delta:LIVERY=Standard:CSL=ModelABC\r\n",
		},
		{
			"Only Required Fields",
			&PlaneInfoResponsePDU{
				From:      "ATC",
				To:        "PILOT",
				Equipment: "B737",
			},
			"#SBATC:PILOT:PI:GEN:EQUIPMENT=B737\r\n",
		},
		{
			"With Airline",
			&PlaneInfoResponsePDU{
				From:      "CTRL",
				To:        "PLANE",
				Equipment: "E170",
				Airline:   "United",
			},
			"#SBCTRL:PLANE:PI:GEN:EQUIPMENT=E170:AIRLINE=United\r\n",
		},
		{
			"With Livery",
			&PlaneInfoResponsePDU{
				From:      "GROUND",
				To:        "ACFT",
				Equipment: "CRJ2",
				Livery:    "BlueSky",
			},
			"#SBGROUND:ACFT:PI:GEN:EQUIPMENT=CRJ2:LIVERY=BlueSky\r\n",
		},
		{
			"With CSL",
			&PlaneInfoResponsePDU{
				From:      "TWR",
				To:        "JET",
				Equipment: "CONC",
				CSL:       "Vintage",
			},
			"#SBTWR:JET:PI:GEN:EQUIPMENT=CONC:CSL=Vintage\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the serialization
			gotStr := tc.pdu.Serialize()

			// Verify the serialized output
			assert.Equal(t, tc.wantStr, gotStr)
		})
	}
}
