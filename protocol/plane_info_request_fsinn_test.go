package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParsePlaneInfoRequestFsinnPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *PlaneInfoRequestFsinnPDU
		wantErr error
	}{
		{
			"Valid full request",
			"#SBPILOT:ATC:FSIPIR:0:BAW:7478:::::B744:British Airways Boeing 747-400\r\n",
			&PlaneInfoRequestFsinnPDU{
				From:                     "PILOT",
				To:                       "ATC",
				AirlineICAO:              "BAW",
				AircraftICAO:             "7478",
				AircraftICAOCombinedType: "B744",
				SendMModel:               "British Airways Boeing 747-400",
			},
			nil,
		},
		{
			"Invalid From",
			"#SB12345678:ATC:FSIPIR:0:UAL:A320:::::A20N:United Airbus A320neo\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid To",
			"#SBPILOT:CONTROLLER123:FSIPIR:0:LUF:B77W:::::B77W:Lufthansa Boeing 777\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Extra delimiter",
			"#SBPILOT:ATC:FSIPIR:0:DLH:::A343:::A343:Lufthansa Airbus A340-300\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing ICAO code",
			"#SBPILOT:ATC:FSIPIR:0::7378:::::B738:Ryanair Boeing 737-800\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid PDU type",
			"#SBPILOT:ATC:FOOBAR:0:SWR:A333:::::A333:Swiss Airbus A330-300\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParsePlaneInfoRequestFsinnPDU(tc.packet)

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

func TestPlaneInfoRequestFsinnPDU_Serialize(t *testing.T) {
	tests := []struct {
		name    string
		pdu     *PlaneInfoRequestFsinnPDU
		wantStr string
	}{
		{
			"Valid Serialize",
			&PlaneInfoRequestFsinnPDU{
				From:                     "PILOT",
				To:                       "ATC",
				AirlineICAO:              "AAA",
				AircraftICAO:             "A320",
				AircraftICAOCombinedType: "A20N",
				SendMModel:               "Airbus A320neo",
			},
			"#SBPILOT:ATC:FSIPIR:0:AAA:A320:::::A20N:Airbus A320neo\r\n",
		},
		// You can add more cases if necessary, for example empty values, very long strings, etc
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantStr, tc.pdu.Serialize())
		})
	}
}
