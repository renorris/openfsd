package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
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
			"$AXPILOT123:ATC01:METAR:KJFK\r\n",
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid To (not alphanumeric)",
			nil,
			"$AXPILOT1:AT*C1:METAR:KJFK\r\n",
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid Station (too long)",
			nil,
			"$AXPILOT1:ATC01:METAR:KJFKKK\r\n",
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing Station",
			nil,
			"$AXPILOT1:ATC01:METAR:\r\n",
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid Command",
			nil,
			"$AXPILOT1:ATC01:NOTAM:KJFK\r\n",
			NewGenericFSDError(SyntaxError),
		},
		{
			"Extra fields",
			nil,
			"$AXPILOT1:ATC01:METAR:KJFK:EXTRA\r\n",
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing Delimiters",
			nil,
			"PILOT1ATC01METARKJFK\r\n",
			NewGenericFSDError(SyntaxError),
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
			result, err := ParseMetarRequestPDU(tc.rawPacket)

			// Check the error
			if tc.expectedError != nil {
				assert.EqualError(t, err, tc.expectedError.Error(), "errors should match expected output for case '%s'", tc.name)
			} else {
				assert.NoError(t, err, "no error should occur for case '%s'", tc.name)
			}

			// Verify the result
			if tc.pduInstance != nil {
				assert.Equal(t, tc.pduInstance, result, "parsed result should match expected PDU for case '%s'", tc.name)
			}
		})
	}
}
