package server

import (
	"math"
	"strings"
	"testing"

	"github.com/renorris/openfsd/internal/sweatbox"
	"github.com/renorris/openfsd/pkg/protocol"
)

func TestEncodeFlightPlanInfo_Golden(t *testing.T) {
	ac := sweatbox.AircraftSnapshot{
		Callsign:  "AAL123",
		Type:      "B738/F",
		Rules:     sweatbox.RulesIFR,
		Dep:       "KBTV",
		Arr:       "KBOS",
		CruiseAlt: 29000,
		Route:     "BTV4 MPV LEB MHT",
		Remarks:   "/v/charts",
		Speed:     0, // parked → default TAS 250
	}
	got := encodeFlightPlanInfo(ac)
	want := "I:B738/F:250:KBTV:0000:0000:29000:KBOS:0:0:0:0::/v/charts:BTV4 MPV LEB MHT"
	if got != want {
		t.Fatalf("encodeFlightPlanInfo =\n%q\nwant\n%q", got, want)
	}

	// Rebuilt $FP query reply shape.
	pkt := buildFileFlightplanPacket("AAL123", "*A", got)
	wantPkt := "$FPAAL123:*A:" + want + "\r\n"
	if pkt != wantPkt {
		t.Fatalf("buildFileFlightplanPacket =\n%q\nwant\n%q", pkt, wantPkt)
	}
}

func TestEncodeFlightPlanInfo_VFRDefaults(t *testing.T) {
	ac := sweatbox.AircraftSnapshot{
		Callsign:  "N4729H",
		Type:      "C172/A",
		Rules:     sweatbox.RulesVFR,
		Dep:       "KBTV",
		Arr:       "KLEB",
		CruiseAlt: 7500,
		Route:     "MPV LEB",
		Remarks:   "/v/vfr",
		Speed:     120, // airborne-ish → use speed as TAS
	}
	got := encodeFlightPlanInfo(ac)
	if !strings.HasPrefix(got, "V:C172/A:120:KBTV:") {
		t.Fatalf("unexpected prefix: %q", got)
	}
	if !strings.Contains(got, ":7500:KLEB:") {
		t.Fatalf("missing cruise/dest: %q", got)
	}
}

func TestPackPitchBankHeading_RoundTrip(t *testing.T) {
	cases := []struct {
		pitch, bank, heading float64
	}{
		{0, 0, 0},
		{0, 0, 90},
		{0, 0, 180},
		{0, 0, 270},
		{0, 0, 359},
		{10, 20, 45},
		{0, 0, 5.9657869012707723}, // from util_test golden decode fixture
	}
	const eps = 0.4 // 10-bit quantisation ~0.35°
	for _, tc := range cases {
		packed := packPitchBankHeading(tc.pitch, tc.bank, tc.heading)
		// Lowest 2 bits always zero.
		if packed&0x3 != 0 {
			t.Fatalf("packed %#x has non-zero low bits", packed)
		}
		p, b, h := pitchBankHeading(packed)
		if math.Abs(p-normDeg(tc.pitch)) > eps ||
			math.Abs(b-normDeg(tc.bank)) > eps ||
			math.Abs(h-normDeg(tc.heading)) > eps {
			t.Errorf("round-trip pitch/bank/hdg = %v/%v/%v → packed=%d → %v/%v/%v",
				tc.pitch, tc.bank, tc.heading, packed, p, b, h)
		}
	}

	// Golden fixture from util_test: decode then re-encode stays close.
	const packedGolden uint32 = 4261294148
	gp, gb, gh := pitchBankHeading(packedGolden)
	re := packPitchBankHeading(gp, gb, gh)
	// Exact re-encode of decoded quanta should match (or be 1 step off due to round).
	rp, rb, rh := pitchBankHeading(re)
	if math.Abs(rp-gp) > 1e-9 || math.Abs(rb-gb) > 1e-9 || math.Abs(rh-gh) > 1e-9 {
		t.Errorf("golden re-encode drift: %v/%v/%v vs %v/%v/%v (packed %d → %d)",
			gp, gb, gh, rp, rb, rh, packedGolden, re)
	}
}

func TestPackPitchBankHeading_InPilotPosition(t *testing.T) {
	pbh := packPitchBankHeading(0, 0, 360) // ≡ 0
	pkt := protocol.PilotPosition{
		TransponderMode:  "N",
		Callsign:         "TEST1",
		TransponderCode:  "1200",
		NetworkRating:    protocol.NetworkRatingObserver,
		Latitude:         44.0,
		Longitude:        -73.0,
		TrueAltitude:     1000,
		Groundspeed:      0,
		PitchBankHeading: pbh,
	}.Marshal()
	parsed, err := protocol.ParsePilotPosition(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.PitchBankHeading != pbh {
		t.Fatalf("pbh = %d, want %d", parsed.PitchBankHeading, pbh)
	}
	_, _, hdg := pitchBankHeading(parsed.PitchBankHeading)
	if math.Abs(hdg) > 0.5 && math.Abs(hdg-360) > 0.5 {
		t.Fatalf("heading decode = %v, want ~0", hdg)
	}
}

func normDeg(d float64) float64 {
	for d < 0 {
		d += 360
	}
	for d >= 360 {
		d -= 360
	}
	return d
}
