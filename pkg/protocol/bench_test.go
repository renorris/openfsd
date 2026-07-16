package protocol

import (
	"testing"
)

var (
	benchPilotPos = []byte("@S:N123AB:1200:1:34.0522:-118.2437:5000:250:4261294148:0\r\n")
	benchATCPos   = []byte("%LAX_TWR:28550:4:50:5:33.9425:-118.4081:0\r\n")
	benchText     = []byte("#TMN123AB:LAX_TWR:hello world\r\n")
	benchCQ       = []byte("$CQN123AB:SERVER:ATC:LAX_TWR\r\n")
)

// BenchmarkTypeOf measures packet type detection from wire prefixes.
func BenchmarkTypeOf(b *testing.B) {
	packets := [][]byte{benchPilotPos, benchATCPos, benchText, benchCQ}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = TypeOf(packets[i%len(packets)])
	}
}

// BenchmarkParsePilotPosition measures full @ position parse.
func BenchmarkParsePilotPosition(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParsePilotPosition(benchPilotPos); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalPilotPosition measures @ position encode.
func BenchmarkMarshalPilotPosition(b *testing.B) {
	p := PilotPosition{
		TransponderMode:  "S",
		Callsign:         "N123AB",
		TransponderCode:  "1200",
		NetworkRating:    NetworkRatingObserver,
		Latitude:         34.0522,
		Longitude:        -118.2437,
		TrueAltitude:     5000,
		Groundspeed:      250,
		PitchBankHeading: 4261294148,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = p.Marshal()
	}
}

// BenchmarkCountFields measures colon field counting.
func BenchmarkCountFields(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = CountFields(benchPilotPos)
	}
}
