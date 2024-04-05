package protocol

import (
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
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
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Out of range value",
			"^PILOT:92.000000:678.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Missing field",
			"^PILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
		},
		{
			"Mismatched type with fields",
			"#STPILOT:12.345678:98.765432:300.00:50.00:4177408112:123.4567:345.6789:-234.5678:111.2222:-333.4444:555.6666:90.00\n\r\n",
			nil,
			NewGenericFSDError(SyntaxError),
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
			result, err := ParseFastPilotPositionPDU(tc.packet)

			// Check the error
			if tc.wantErr != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}

			// Verify the result
			if tc.want != nil {
				assert.Equal(t, tc.want.From, result.From)
				assert.InDelta(t, tc.want.Lat, result.Lat, 1e-6)
				assert.InDelta(t, tc.want.Lng, result.Lng, 1e-6)
				assert.InDelta(t, tc.want.AltitudeTrue, result.AltitudeTrue, 1e-2)
				assert.InDelta(t, tc.want.AltitudeAgl, result.AltitudeAgl, 1e-2)
				assert.InDelta(t, tc.want.Pitch, result.Pitch, 1)
				assert.InDelta(t, tc.want.Bank, result.Bank, 1)
				assert.InDelta(t, tc.want.Heading, result.Heading, 1)
				assert.InDelta(t, tc.want.PositionalVelocityVector.X, result.PositionalVelocityVector.X, 1e-4)
				assert.InDelta(t, tc.want.PositionalVelocityVector.Y, result.PositionalVelocityVector.Y, 1e-4)
				assert.InDelta(t, tc.want.PositionalVelocityVector.Z, result.PositionalVelocityVector.Z, 1e-4)
				assert.InDelta(t, tc.want.RotationalVelocityVector.X, result.RotationalVelocityVector.X, 1e-4)
				assert.InDelta(t, tc.want.RotationalVelocityVector.Y, result.RotationalVelocityVector.Y, 1e-4)
				assert.InDelta(t, tc.want.RotationalVelocityVector.Z, result.RotationalVelocityVector.Z, 1e-4)
				assert.InDelta(t, tc.want.NoseGearAngle, result.NoseGearAngle, 1e-2)
			} else {
				assert.Nil(t, result)
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
