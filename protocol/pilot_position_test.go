package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParsePilotPositionPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *PilotPositionPDU
		wantErr error
	}{
		{
			"Valid (squawk standby)",
			"@S:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{
				SquawkingModeC:   false,
				Identing:         false,
				From:             "N123",
				SquawkCode:       "1200",
				NetworkRating:    1,
				Lat:              40.6452667,
				Lng:              -73.7738611,
				TrueAltitude:     16,
				GroundSpeed:      0,
				Pitch:            10,
				Bank:             10,
				Heading:          10,
				PressureAltitude: 352,
			},
			nil,
		},
		{
			"Valid (squawk normal)",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{
				SquawkingModeC:   true,
				Identing:         false,
				From:             "N123",
				SquawkCode:       "1200",
				NetworkRating:    1,
				Lat:              40.6452667,
				Lng:              -73.7738611,
				TrueAltitude:     16,
				GroundSpeed:      0,
				Pitch:            10,
				Bank:             10,
				Heading:          10,
				PressureAltitude: 352,
			},
			nil,
		},
		{
			"Valid (squawk ident)",
			"@Y:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{
				SquawkingModeC:   true,
				Identing:         true,
				From:             "N123",
				SquawkCode:       "1200",
				NetworkRating:    1,
				Lat:              40.6452667,
				Lng:              -73.7738611,
				TrueAltitude:     16,
				GroundSpeed:      0,
				Pitch:            10,
				Bank:             10,
				Heading:          10,
				PressureAltitude: 352,
			},
			nil,
		},
		{
			"Invalid transponder mode",
			"@foo:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing transponder mode",
			"@:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid callsign",
			"@N:N172SP99:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing callsign",
			"@N::1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Invalid transponder code length",
			"@N:N123:00000:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Out of range transponder code",
			"@N:N123:7778:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing transponder code",
			"@N:N123::1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Network rating too low",
			"@N:N123:1200:0:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Network rating too high",
			"@N:N123:1200:13:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"NaN network rating",
			"@N:N123:1200:foo:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Out of bounds lat",
			"@N:N123:1200:1:-91.1231:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing lat",
			"@N:N123:1200:1::-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"NaN lat",
			"@N:N123:1200:1:foo:-73.7738611:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Out of bounds lng",
			"@N:N123:1200:1:40.6452667:181.32133:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing lng",
			"@N:N123:1200:1:40.6452667::16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"NaN lng",
			"@N:N123:1200:1:40.6452667:foo:16:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Too low true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:-1501:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Too high true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:100000:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611::0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"NaN true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:foo:0:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Negative groundspeed",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:-1:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Unrealistic groundspeed",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:10000:4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing groundspeed",
			"@N:N123:1200:1:40.6452667:-73.7738611:16::4177408112:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"pbh uint32 too long",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4294967296:336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"pbh missing",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0::336\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Pressure alt too high",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:999999\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Pressure alt too low",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:-10016\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing pressure alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Empty packet",
			"@\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			result, err := ParsePilotPositionPDU(tc.packet)

			// Check the error
			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			if tc.want != nil {
				assert.Equal(t, tc.want.SquawkingModeC, result.SquawkingModeC)
				assert.Equal(t, tc.want.Identing, result.Identing)
				assert.Equal(t, tc.want.From, result.From)
				assert.Equal(t, tc.want.SquawkCode, result.SquawkCode)
				assert.Equal(t, tc.want.NetworkRating, result.NetworkRating)
				assert.InDelta(t, tc.want.Lat, result.Lat, 1e-3)
				assert.InDelta(t, tc.want.Lng, result.Lng, 1e-3)
				assert.Equal(t, tc.want.TrueAltitude, result.TrueAltitude)
				assert.Equal(t, tc.want.GroundSpeed, result.GroundSpeed)
				assert.InDelta(t, tc.want.Pitch, result.Pitch, 1)
				assert.InDelta(t, tc.want.Bank, result.Bank, 1)
				assert.InDelta(t, tc.want.Heading, result.Heading, 1)
				assert.Equal(t, tc.want.PressureAltitude, result.PressureAltitude)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}
