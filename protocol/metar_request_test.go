package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestMetarRequestPDU_SerializationAndParsing(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name          string
		pduInstance   *MetarRequestPDU
		rawPacket     string
		expectedError error
	}{
		{
			"Valid MetarRequestPDU",
			&MetarRequestPDU{
				From:    "PILOT1",
				To:      "ATC01",
				Station: "KJFK",
			},
			"$AXPILOT1:ATC01:METAR:KJFK\r\n",
			nil,
		},
		{
			"Invalid From (too long)",
			nil,
			"$AXPILOT123PILOT123PILOT123PILOT123PILOT123:ATC01:METAR:KJFK\r\n",
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid To (not alphanumeric)",
			nil,
			"$AXPILOT1:AT*C1:METAR:KJFK\r\n",
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid Station (too long)",
			nil,
			"$AXPILOT1:ATC01:METAR:KJFKKK\r\n",
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing Station",
			nil,
			"$AXPILOT1:ATC01:METAR:\r\n",
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid Command",
			nil,
			"$AXPILOT1:ATC01:NOTAM:KJFK\r\n",
			NewGenericFSDError(SyntaxError, "NOTAM", "third parameter must be 'METAR'"),
		},
		{
			"Extra fields",
			nil,
			"$AXPILOT1:ATC01:METAR:KJFK:EXTRA\r\n",
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Missing Delimiters",
			nil,
			"PILOT1ATC01METARKJFK\r\n",
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pduInstance != nil {
				// Test serialization
				serialized := tc.pduInstance.Serialize()
				assert.Equal(t, tc.rawPacket, serialized, "serialization should match expected output")
			}

			// Perform parsing
			pdu := MetarRequestPDU{}
			err := pdu.Parse(tc.rawPacket)

			// Check the error
			if tc.expectedError != nil {
				if strings.Contains(tc.expectedError.Error(), "validation error") {
					assert.Contains(t, err.Error(), "validation error")
				} else {
					assert.EqualError(t, err, tc.expectedError.Error())
				}
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			if tc.pduInstance != nil {
				assert.Equal(t, tc.pduInstance, &pdu, "parsed result should match expected PDU for case '%s'", tc.name)
			}
		})
	}
}
