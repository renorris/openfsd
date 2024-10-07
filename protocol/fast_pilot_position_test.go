package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestParseFastPilotPositionPDU(t *testing.T) {
	V = validator.New(validator.WithRequiredStructEnabled())

	tests := []struct {
		name    string
		packet  string
		want    *FastPilotPositionPDU
		wantErr error
	}{
		{
			"Valid Fast Type",
			"^PILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
			&FastPilotPositionPDU{
				Type:         FastPilotPositionTypeFast,
				From:         "PILOT",
				Lat:          12.345678,
				Lng:          98.765432,
				AltitudeTrue: 300,
				AltitudeAgl:  50,
				Pitch:        10, // Assuming appropriate conversion function
				Heading:      10,
				Bank:         10,
				PositionalVelocityVector: VelocityVector{
					X: 123.4567,
					Y: 345.6789,
					Z: -234.5678,
				},
				RotationalVelocityVector: VelocityVector{
					X: 111.2222,
					Y: -333.4444,
					Z: 555.6666,
				},
				NoseGearAngle: 90.00,
			},
			nil,
		},
		{
			"Valid Slow Type",
			"#SLPILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
			&FastPilotPositionPDU{
				Type:         FastPilotPositionTypeSlow,
				From:         "PILOT",
				Lat:          12.345678,
				Lng:          98.765432,
				AltitudeTrue: 300,
				AltitudeAgl:  50,
				Pitch:        10, // Assuming appropriate conversion function
				Heading:      10,
				Bank:         10,
				PositionalVelocityVector: VelocityVector{
					X: 123.4567,
					Y: 345.6789,
					Z: -234.5678,
				},
				RotationalVelocityVector: VelocityVector{
					X: 111.2222,
					Y: -333.4444,
					Z: 555.6666,
				},
				NoseGearAngle: 90.00,
			},
			nil,
		},
		{
			"Invalid Type",
			"?PILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
			&FastPilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid packet prefix"),
		},
		{
			"Out of range value",
			"^PILOT:92.000000:678.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
			&FastPilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "validation error"),
		},
		{
			"Missing field",
			"^PILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444\r\n",
			&FastPilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Mismatched type with fields",
			"#STPILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\n\r\n",
			&FastPilotPositionPDU{},
			NewGenericFSDError(SyntaxError, "", "invalid parameter count"),
		},
		{
			"Valid Stopped Type",
			"#STPILOT:12.345678:98.765432:300.00:50.00:4177408112:0.00\r\n",
			&FastPilotPositionPDU{
				Type:         FastPilotPositionTypeStopped,
				From:         "PILOT",
				Lat:          12.345678,
				Lng:          98.765432,
				AltitudeTrue: 300,
				AltitudeAgl:  50,
				Pitch:        10,
				Heading:      10,
				Bank:         10,
				PositionalVelocityVector: VelocityVector{
					X: 0,
					Y: 0,
					Z: 0,
				},
				RotationalVelocityVector: VelocityVector{
					X: 0,
					Y: 0,
					Z: 0,
				},
				NoseGearAngle: 0.00,
			},
			nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Perform the parsing
			pdu := FastPilotPositionPDU{}
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
				assert.Equal(t, tc.want.From, pdu.From)
				assert.InDelta(t, tc.want.Lat, pdu.Lat, 1e-6)
				assert.InDelta(t, tc.want.Lng, pdu.Lng, 1e-6)
				assert.InDelta(t, tc.want.AltitudeTrue, pdu.AltitudeTrue, 1e-2)
				assert.InDelta(t, tc.want.AltitudeAgl, pdu.AltitudeAgl, 1e-2)
				assert.InDelta(t, tc.want.Pitch, pdu.Pitch, 1)
				assert.InDelta(t, tc.want.Bank, pdu.Bank, 1)
				assert.InDelta(t, tc.want.Heading, pdu.Heading, 1)
				assert.InDelta(t, tc.want.PositionalVelocityVector.X, pdu.PositionalVelocityVector.X, 1e-4)
				assert.InDelta(t, tc.want.PositionalVelocityVector.Y, pdu.PositionalVelocityVector.Y, 1e-4)
				assert.InDelta(t, tc.want.PositionalVelocityVector.Z, pdu.PositionalVelocityVector.Z, 1e-4)
				assert.InDelta(t, tc.want.RotationalVelocityVector.X, pdu.RotationalVelocityVector.X, 1e-4)
				assert.InDelta(t, tc.want.RotationalVelocityVector.Y, pdu.RotationalVelocityVector.Y, 1e-4)
				assert.InDelta(t, tc.want.RotationalVelocityVector.Z, pdu.RotationalVelocityVector.Z, 1e-4)
				assert.InDelta(t, tc.want.NoseGearAngle, pdu.NoseGearAngle, 1e-2)
			} else {
				assert.Nil(t, &pdu)
			}
		})
	}
}

func TestFastPilotPositionPDUSerialize(t *testing.T) {
	tests := []struct {
		name string
		pdu  *FastPilotPositionPDU
		want string
	}{
		{
			"Serialize Fast",
			&FastPilotPositionPDU{
				Type:         FastPilotPositionTypeFast,
				From:         "PILOT",
				Lat:          12.345678,
				Lng:          98.765432,
				AltitudeTrue: 300,
				AltitudeAgl:  50,
				Pitch:        10,
				Heading:      10,
				Bank:         10,
				PositionalVelocityVector: VelocityVector{
					X: 123.4567,
					Y: 345.6789,
					Z: -234.5678,
				},
				RotationalVelocityVector: VelocityVector{
					X: 111.2222,
					Y: -333.4444,
					Z: 555.6666,
				},
				NoseGearAngle: 90.00,
			},
			"^PILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
		},
		{
			"Serialize Slow",
			&FastPilotPositionPDU{
				Type:         FastPilotPositionTypeSlow,
				From:         "PILOT",
				Lat:          12.345678,
				Lng:          98.765432,
				AltitudeTrue: 300,
				AltitudeAgl:  50,
				Pitch:        10,
				Heading:      10,
				Bank:         10,
				PositionalVelocityVector: VelocityVector{
					X: 123.4567,
					Y: 345.6789,
					Z: -234.5678,
				},
				RotationalVelocityVector: VelocityVector{
					X: 111.2222,
					Y: -333.4444,
					Z: 555.6666,
				},
				NoseGearAngle: 90.00,
			},
			"#SLPILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
		},
		{
			"Serialize Stopped",
			&FastPilotPositionPDU{
				Type:         FastPilotPositionTypeStopped,
				From:         "PILOT",
				Lat:          12.345678,
				Lng:          98.765432,
				AltitudeTrue: 300,
				AltitudeAgl:  50,
				Pitch:        10,
				Heading:      10,
				Bank:         10,
				PositionalVelocityVector: VelocityVector{
					X: 0,
					Y: 0,
					Z: 0,
				},
				RotationalVelocityVector: VelocityVector{
					X: 0,
					Y: 0,
					Z: 0,
				},
				NoseGearAngle: 90.00,
			},
			"#STPILOT:12.345678:98.765432:300.00:50.00:4177408112:90.00\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Perform serialization
			got := tt.pdu.Serialize()

			// Verify result
			assert.Equal(t, tt.want, got)
		})
	}
}
