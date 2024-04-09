package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParseMetarResponsePDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *MetarResponsePDU
		wantErr error
	}{
		{
			"Valid - Standard Metar",
			"$ARSERVER:CLIENT:KSEE 091847Z 25007KT 10SM SKC 24/04 A3006\r\n",
			&MetarResponsePDU{
				From:  "SERVER",
				To:    "CLIENT",
				Metar: "KSEE 091847Z 25007KT 10SM SKC 24/04 A3006",
			},
			nil,
		},
		{
			"Valid - colons in metar",
			"$ARSERVER:CLIENT:KSEE 091847Z::: 25007KT 10::SM SKC 24/:04 A3006\r\n",
			&MetarResponsePDU{
				From:  "SERVER",
				To:    "CLIENT",
				Metar: "KSEE 091847Z::: 25007KT 10::SM SKC 24/:04 A3006",
			},
			nil,
		},
		{
			"Missing To field",
			"$ARSERVER::KSEE 091847Z 25007KT 10SM SKC 24/04 A3006\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"From Field too long",
			"$ARSERVERTOLONG:CLIENT:KSEE 091847Z 25007KT 10SM SKC 24/04 A3006\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Metar too long",
			"$ARSERVER:CLIENT:" + strings.Repeat("A", 257) + "\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Incomplete packet format",
			"$ARSERVER:CLIENT\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Empty metar field",
			"$ARSERVER:CLIENT:\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParseMetarResponsePDU(tc.packet)

			// Check the error
			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			assert.Equal(t, tc.want, result)
		})
	}
}

func TestMetarResponsePDU_Serialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  MetarResponsePDU
		want string
	}{
		{
			"Standard Metar",
			MetarResponsePDU{
				From:  "SERVER",
				To:    "CLIENT",
				Metar: "KSEE 091847Z 25007KT 10SM SKC 24/04 A3006",
			},
			"$ARSERVER:CLIENT:KSEE 091847Z 25007KT 10SM SKC 24/04 A3006\r\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the serialization
			got := tc.pdu.Serialize()

			// Verify the result
			assert.Equal(t, tc.want, got)
		})
	}
}
