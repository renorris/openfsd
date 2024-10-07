package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
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
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "foo", "invalid transponder state identifier"),
		},
		{
			"Missing transponder mode",
			"@:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid transponder state identifier"),
		},
		{
			"From field too long",
			"@N:N172SP99N172SP99N172SP99N172SP99:1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing callsign",
			"@N::1200:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Invalid transponder code length",
			"@N:N123:00000:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Out of range transponder code",
			"@N:N123:7778:1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "7778", "invalid transponder code"),
		},
		{
			"Missing transponder code",
			"@N:N123::1:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Network rating too low",
			"@N:N123:1200:0:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Network rating too high",
			"@N:N123:1200:13:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"NaN network rating",
			"@N:N123:1200:foo:40.6452667:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "foo", "invalid network rating"),
		},
		{
			"Out of bounds lat",
			"@N:N123:1200:1:-91.1231:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing lat",
			"@N:N123:1200:1::-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid latitude"),
		},
		{
			"NaN lat",
			"@N:N123:1200:1:foo:-73.7738611:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "foo", "invalid latitude"),
		},
		{
			"Out of bounds lng",
			"@N:N123:1200:1:40.6452667:181.32133:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing lng",
			"@N:N123:1200:1:40.6452667::16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid longitude"),
		},
		{
			"NaN lng",
			"@N:N123:1200:1:40.6452667:foo:16:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "foo", "invalid longitude"),
		},
		{
			"Too low true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:-1501:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Too high true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:100000:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611::0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid true altitude"),
		},
		{
			"NaN true alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:foo:0:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "foo", "invalid true altitude"),
		},
		{
			"Negative groundspeed",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:-1:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Unrealistic groundspeed",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:10000:4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing groundspeed",
			"@N:N123:1200:1:40.6452667:-73.7738611:16::4177408112:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid groundspeed"),
		},
		{
			"pbh uint32 too long",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4294967296:336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "4294967296", "invalid pitch/bank/heading integer"),
		},
		{
			"pbh missing",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0::336\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid pitch/bank/heading integer"),
		},
		{
			"Pressure alt too high",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:999999\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Pressure alt too low",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:-10016\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing pressure alt",
			"@N:N123:1200:1:40.6452667:-73.7738611:16:0:4177408112:\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid pressure altitude"),
		},
		{
			"Empty packet",
			"@\r\n",
			&PilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := PilotPositionPDU{}
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
			if tc.want != nil {
				assert.Equal(t, tc.want.SquawkingModeC, pdu.SquawkingModeC)
				assert.Equal(t, tc.want.Identing, pdu.Identing)
				assert.Equal(t, tc.want.From, pdu.From)
				assert.Equal(t, tc.want.SquawkCode, pdu.SquawkCode)
				assert.Equal(t, tc.want.NetworkRating, pdu.NetworkRating)
				assert.InDelta(t, tc.want.Lat, pdu.Lat, 1e-3)
				assert.InDelta(t, tc.want.Lng, pdu.Lng, 1e-3)
				assert.Equal(t, tc.want.TrueAltitude, pdu.TrueAltitude)
				assert.Equal(t, tc.want.GroundSpeed, pdu.GroundSpeed)
				assert.InDelta(t, tc.want.Pitch, pdu.Pitch, 1)
				assert.InDelta(t, tc.want.Bank, pdu.Bank, 1)
				assert.InDelta(t, tc.want.Heading, pdu.Heading, 1)
				assert.Equal(t, tc.want.PressureAltitude, pdu.PressureAltitude)
			} else {
				assert.Nil(t, &pdu)
			}
		})
	}
}
