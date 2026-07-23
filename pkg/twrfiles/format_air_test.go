package twrfiles

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFormatAIR_KBTVGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.air"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rows, errs := ParseAIR(string(raw))
	if len(errs) != 0 {
		t.Fatalf("parse errs: %v", errs)
	}
	got := FormatAIR(rows)
	want, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.formatted.air"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("FormatAIR golden mismatch\n--- got ---\n%q\n--- want ---\n%q", got, string(want))
	}
	if strings.Contains(got, "\r") {
		t.Error("formatted AIR contains CR")
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("formatted AIR missing trailing newline")
	}
	// No legend comments.
	if strings.Contains(got, ";") {
		t.Error("FormatAIR must not emit comment legend by default")
	}
}

func TestFormatAIR_RoundTripStructural(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "KBTV_example.air"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	rows1, errs := ParseAIR(string(raw))
	if len(errs) != 0 {
		t.Fatalf("parse1 errs: %v", errs)
	}
	text := FormatAIR(rows1)
	rows2, errs2 := ParseAIR(text)
	if len(errs2) != 0 {
		t.Fatalf("parse2 errs: %v", errs2)
	}
	if !reflect.DeepEqual(rows1, rows2) {
		t.Errorf("round-trip mismatch\n  rows1=%+v\n  rows2=%+v", rows1, rows2)
	}
	if text2 := FormatAIR(rows2); text != text2 {
		t.Error("FormatAIR not idempotent after re-parse")
	}
}

func TestFormatAIR_EmptyList(t *testing.T) {
	got := FormatAIR(nil)
	if got != "" {
		t.Errorf("nil list = %q, want empty", got)
	}
	got = FormatAIR([]Aircraft{})
	if got != "" {
		t.Errorf("empty list = %q, want empty", got)
	}
}

func TestFormatAIR_SixteenFieldsAndUppercase(t *testing.T) {
	rows := []Aircraft{
		{
			Callsign:  "aal99",
			Type:      "b738/f",
			Engine:    "j",
			Rules:     "i",
			Dep:       "kbtv",
			Arr:       "kbos",
			CruiseAlt: 29000,
			Route:     "BTV4 MPV",
			Remarks:   "/v/charts",
			Squawk:    "2200",
			XPDRMode:  "s",
			Lat:       44.469758,
			Lon:       -73.154747,
			Alt:       335,
			Speed:     0,
			Heading:   360,
		},
	}
	got := FormatAIR(rows)
	line := strings.TrimSuffix(got, "\n")
	fields := strings.Split(line, ":")
	if len(fields) != 16 {
		t.Fatalf("fields = %d, want 16: %q", len(fields), line)
	}
	if fields[0] != "AAL99" || fields[1] != "B738/F" || fields[2] != "J" || fields[3] != "I" {
		t.Errorf("upper fields wrong: %v", fields[:4])
	}
	if fields[4] != "KBTV" || fields[5] != "KBOS" {
		t.Errorf("dep/arr = %s/%s", fields[4], fields[5])
	}
	if fields[6] != "29000" {
		t.Errorf("cruise = %q", fields[6])
	}
	if fields[7] != "BTV4 MPV" || fields[8] != "/v/charts" {
		t.Errorf("route/remarks altered: %q %q", fields[7], fields[8])
	}
	if fields[10] != "S" {
		t.Errorf("xpdr = %q", fields[10])
	}
	if fields[11] != "44.469758" || fields[12] != "-73.154747" {
		t.Errorf("lat/lon = %s/%s", fields[11], fields[12])
	}
	if fields[13] != "335" || fields[14] != "0" || fields[15] != "360" {
		t.Errorf("alt/spd/hdg = %s/%s/%s", fields[13], fields[14], fields[15])
	}
}

func TestFormatAIR_CompactVsFractional(t *testing.T) {
	rows := []Aircraft{
		{
			Callsign:  "N1",
			Type:      "C172",
			Engine:    EnginePiston,
			Rules:     RulesVFR,
			Dep:       "KBTV",
			Arr:       "KLEB",
			CruiseAlt: 3500,
			Route:     "DCT",
			Remarks:   "",
			Squawk:    "1200",
			XPDRMode:  XPDRModeNormal,
			Lat:       44.5,
			Lon:       -73.1,
			Alt:       335.5, // fractional
			Speed:     90.25,
			Heading:   180.0, // whole
		},
	}
	got := FormatAIR(rows)
	line := strings.TrimSuffix(got, "\n")
	fields := strings.Split(line, ":")
	if len(fields) != 16 {
		t.Fatalf("fields = %d: %q", len(fields), line)
	}
	if fields[11] != "44.500000" || fields[12] != "-73.100000" {
		t.Errorf("lat/lon 6dp = %s %s", fields[11], fields[12])
	}
	if fields[13] != "335.5" {
		t.Errorf("alt fractional = %q", fields[13])
	}
	if fields[14] != "90.25" {
		t.Errorf("speed fractional = %q", fields[14])
	}
	if fields[15] != "180" {
		t.Errorf("heading whole = %q", fields[15])
	}
	// Route/remarks preserved empty is fine (empty field).
	if fields[8] != "" {
		t.Errorf("remarks = %q", fields[8])
	}
}

func TestFormatAIR_PreservesRouteRemarksAsStored(t *testing.T) {
	rows := []Aircraft{
		{
			Callsign:  "X1",
			Type:      "A320",
			Engine:    EngineJet,
			Rules:     RulesIFR,
			Dep:       "KBTV",
			Arr:       "KBOS",
			CruiseAlt: 10000,
			Route:     "mixed Case Route",
			Remarks:   "keep /v mixed",
			Squawk:    "1234",
			XPDRMode:  XPDRModeStandby,
			Lat:       1, Lon: 2, Alt: 3, Speed: 4, Heading: 5,
		},
	}
	got := FormatAIR(rows)
	if !strings.Contains(got, ":mixed Case Route:keep /v mixed:") {
		t.Errorf("route/remarks not preserved as-is: %q", got)
	}
}
