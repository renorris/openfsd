package twrfiles

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFormatAPT_KBTVGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.apt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	apt, errs := ParseAPT(string(raw))
	if len(errs) != 0 {
		t.Fatalf("parse errs: %v", errs)
	}
	got := FormatAPT(apt)
	want, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.formatted.apt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("FormatAPT golden mismatch\n--- got (%d bytes) ---\n%s\n--- want (%d bytes) ---\n%s",
			len(got), truncate(got, 800), len(want), truncate(string(want), 800))
		gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
		n := len(gl)
		if len(wl) < n {
			n = len(wl)
		}
		for i := 0; i < n; i++ {
			if gl[i] != wl[i] {
				t.Errorf("first diff at line %d:\n  got:  %q\n  want: %q", i+1, gl[i], wl[i])
				break
			}
		}
		if len(gl) != len(wl) {
			t.Errorf("line count got=%d want=%d", len(gl), len(wl))
		}
	}
	if strings.Contains(got, "\r") {
		t.Error("formatted APT contains CR")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("formatted APT missing trailing newline")
	}
}

func TestFormatAPT_RoundTripStructural(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.apt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	apt1, errs := ParseAPT(string(raw))
	if len(errs) != 0 {
		t.Fatalf("parse1 errs: %v", errs)
	}
	text := FormatAPT(apt1)
	apt2, errs2 := ParseAPT(text)
	if len(errs2) != 0 {
		t.Fatalf("parse2 errs: %v", errs2)
	}
	if !reflect.DeepEqual(apt1, apt2) {
		t.Errorf("round-trip structural mismatch\n  surfaces1=%d surfaces2=%d icao1=%q icao2=%q",
			len(apt1.Surfaces), len(apt2.Surfaces), apt1.ICAO, apt2.ICAO)
		for i := range apt1.Surfaces {
			if i >= len(apt2.Surfaces) {
				break
			}
			if !reflect.DeepEqual(apt1.Surfaces[i], apt2.Surfaces[i]) {
				t.Errorf("surface[%d] mismatch:\n  got  %+v\n  want %+v", i, apt2.Surfaces[i], apt1.Surfaces[i])
			}
		}
	}
	// Format is stable: Format(Parse(Format(x))) == Format(x)
	if text2 := FormatAPT(apt2); text != text2 {
		t.Error("Format not idempotent after re-parse")
	}
}

func TestFormatAPT_EmptyAirportDefaults(t *testing.T) {
	got := FormatAPT(Airport{})
	want := "" +
		"icao=\n" +
		"magnetic variation=0\n" +
		"field elevation=0\n" +
		"pattern elevation=0\n" +
		"pattern size=1\n" +
		"initial climb props=3000\n" +
		"initial climb jets=5000\n" +
		"jet airlines=\n" +
		"turboprop airlines=\n" +
		"registration=N\n" +
		"\n"
	if got != want {
		t.Errorf("empty airport format:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}

	// Empty ICAO yields a validation warning; defaults still apply.
	apt, errs := ParseAPT(got)
	if len(errs) != 1 || !strings.Contains(errs[0], "ICAO") {
		t.Fatalf("re-parse errs = %v, want single ICAO length warning", errs)
	}
	if apt.PatternSize != 1 || apt.InitClimbProps != 3000 || apt.InitClimbJets != 5000 || apt.Registration != "N" {
		t.Errorf("defaults not recovered: size=%v props=%v jets=%v reg=%q",
			apt.PatternSize, apt.InitClimbProps, apt.InitClimbJets, apt.Registration)
	}
}

func TestFormatAPT_ParkingOnly(t *testing.T) {
	apt := Airport{
		ICAO:           "kbtv", // lower → upper on format
		PatternSize:    1,
		InitClimbProps: 3000,
		InitClimbJets:  5000,
		Registration:   "N",
		Surfaces: []Surface{
			{
				Kind:   SurfaceParking,
				Name:   "G1",
				Points: []Point{{Lat: 44.46893, Lon: -73.15392}},
			},
		},
	}
	got := FormatAPT(apt)
	if !strings.Contains(got, "icao=KBTV\n") {
		t.Errorf("ICAO not uppercased: %q", got)
	}
	if !strings.Contains(got, "[PARKING G1]\n44.468930 -73.153920\n") {
		t.Errorf("parking block missing/wrong:\n%s", got)
	}
	if strings.Contains(got, "displaced") || strings.Contains(got, "turnoff") {
		t.Error("parking-only should not emit runway options")
	}
	if strings.Contains(got, "TAXIWAY") || strings.Contains(got, "HOLD") || strings.Contains(got, "RUNWAY") {
		t.Error("unexpected section kinds")
	}
}

func TestFormatAPT_RunwayTurnoffLeftRight(t *testing.T) {
	apt := Airport{
		ICAO:           "TEST",
		PatternSize:    1,
		InitClimbProps: 3000,
		InitClimbJets:  5000,
		Registration:   "N",
		Surfaces: []Surface{
			{
				Kind:        SurfaceRunway,
				Name:        "19/1",
				RwyA:        "19",
				RwyB:        "1",
				DispA:       0,
				DispB:       0,
				TurnoffLeft: true,
				Points: []Point{
					{Lat: 1.0, Lon: 2.0},
					{Lat: 3.0, Lon: 4.0},
				},
			},
			{
				Kind:        SurfaceRunway,
				Name:        "10/28",
				RwyA:        "10",
				RwyB:        "28",
				DispA:       100,
				DispB:       200,
				TurnoffLeft: false,
				Points: []Point{
					{Lat: 5.5, Lon: 6.5},
					{Lat: 7.25, Lon: 8.75},
				},
			},
		},
	}
	got := FormatAPT(apt)

	if !strings.Contains(got, "[RUNWAY 19/1]\ndisplaced threshold=0/0\nturnoff=left\n") {
		t.Errorf("left turnoff / zero disp missing:\n%s", got)
	}
	if !strings.Contains(got, "[RUNWAY 10/28]\ndisplaced threshold=100/200\nturnoff=right\n") {
		t.Errorf("right turnoff / disp missing:\n%s", got)
	}
	if !strings.Contains(got, "1.000000 2.000000\n") || !strings.Contains(got, "7.250000 8.750000\n") {
		t.Errorf("coord formatting wrong:\n%s", got)
	}

	i19 := strings.Index(got, "[RUNWAY 19/1]")
	i10 := strings.Index(got, "[RUNWAY 10/28]")
	if i19 < 0 || i10 < 0 || i19 > i10 {
		t.Error("surface order not preserved")
	}

	// Round-trip turnoff flags.
	parsed, errs := ParseAPT(got)
	if len(errs) != 0 {
		t.Fatalf("re-parse errs: %v", errs)
	}
	if len(parsed.Surfaces) != 2 {
		t.Fatalf("surfaces = %d", len(parsed.Surfaces))
	}
	if !parsed.Surfaces[0].TurnoffLeft {
		t.Error("surface0 TurnoffLeft want true")
	}
	if parsed.Surfaces[1].TurnoffLeft {
		t.Error("surface1 TurnoffLeft want false")
	}
	if parsed.Surfaces[1].DispA != 100 || parsed.Surfaces[1].DispB != 200 {
		t.Errorf("disp = %v/%v", parsed.Surfaces[1].DispA, parsed.Surfaces[1].DispB)
	}
}

func TestFormatAPT_SurfaceOrderNotRebucketed(t *testing.T) {
	apt := Airport{
		ICAO:           "XXXX",
		PatternSize:    1,
		InitClimbProps: 3000,
		InitClimbJets:  5000,
		Registration:   "N",
		Surfaces: []Surface{
			{Kind: SurfaceTaxiway, Name: "A", Points: []Point{{1.1, 2.2}, {3.3, 4.4}}},
			{Kind: SurfaceParking, Name: "P1", Points: []Point{{5.5, 6.6}}},
			{Kind: SurfaceHold, Name: "H1", Points: []Point{{7.7, 8.8}}},
			{Kind: SurfaceRunway, Name: "1/19", RwyA: "1", RwyB: "19", TurnoffLeft: true,
				Points: []Point{{9.9, 10.1}, {11.1, 12.2}}},
		},
	}
	got := FormatAPT(apt)
	order := []string{"[TAXIWAY A]", "[PARKING P1]", "[HOLD H1]", "[RUNWAY 1/19]"}
	prev := -1
	for _, marker := range order {
		idx := strings.Index(got, marker)
		if idx < 0 {
			t.Fatalf("missing %s in:\n%s", marker, got)
		}
		if idx < prev {
			t.Errorf("out of order: %s", marker)
		}
		prev = idx
	}
}

func TestFormatAPT_HeaderFloatCompact(t *testing.T) {
	apt := Airport{
		ICAO:           "KXYZ",
		MagVar:         -14.5,
		FieldElev:      12.25,
		PatternElev:    100.0,
		PatternSize:    1.5,
		InitClimbProps: 2500.5,
		InitClimbJets:  4000,
		Registration:   "C",
	}
	got := FormatAPT(apt)
	// Compact forms, not forced 6-decimal for headers.
	for _, line := range []string{
		"magnetic variation=-14.5",
		"field elevation=12.25",
		"pattern elevation=100",
		"pattern size=1.5",
		"initial climb props=2500.5",
		"initial climb jets=4000",
		"registration=C",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("missing header line %q in:\n%s", line, got)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
