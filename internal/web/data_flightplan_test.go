package web

import (
	"encoding/json"
	"testing"

	"github.com/renorris/openfsd/internal/serviceapi"
)

func TestFormatHHMM(t *testing.T) {
	cases := []struct {
		h, m, want string
	}{
		{"0", "0", "0000"},
		{"2", "5", "0205"},
		{"2", "40", "0240"},
		{"", "", "0000"},
		{"x", "3", "0003"},
		{"-1", "5", "0005"},
		{"12", "y", "1200"},
	}
	for _, tc := range cases {
		got := formatHHMM(tc.h, tc.m)
		if got != tc.want {
			t.Errorf("formatHHMM(%q,%q)=%q want %q", tc.h, tc.m, got, tc.want)
		}
	}
}

func TestMapInfoSectionToDatafeedFP(t *testing.T) {
	if got := mapInfoSectionToDatafeedFP("", "1200"); got != nil {
		t.Fatalf("empty info → nil, got %#v", got)
	}
	info := "I:B738:450:KLAX:1200:0000:FL350:KJFK:2:40:5:30:KBOS:RMK:DCT"
	fp := mapInfoSectionToDatafeedFP(info, "4321")
	if fp == nil {
		t.Fatal("expected non-nil FP")
	}
	if fp.FlightRules != "I" || fp.Aircraft != "B738" || fp.AircraftFAA != "B738" || fp.AircraftShort != "B738" {
		t.Fatalf("type/rules: %#v", fp)
	}
	if fp.Departure != "KLAX" || fp.Arrival != "KJFK" || fp.Alternate != "KBOS" {
		t.Fatalf("airports: %#v", fp)
	}
	if fp.DepTime != "1200" {
		t.Fatalf("deptime=%q", fp.DepTime)
	}
	if fp.EnrouteTime != "0240" || fp.FuelTime != "0530" {
		t.Fatalf("times enroute=%q fuel=%q", fp.EnrouteTime, fp.FuelTime)
	}
	if fp.Remarks != "RMK" || fp.Route != "DCT" {
		t.Fatalf("remarks/route: %#v", fp)
	}
	if fp.AssignedTransponder != "4321" || fp.RevisionID != 0 {
		t.Fatalf("beacon/rev: %#v", fp)
	}
	// Partial fields: map what is present
	partial := mapInfoSectionToDatafeedFP("V:C172", "")
	if partial == nil || partial.FlightRules != "V" || partial.Aircraft != "C172" {
		t.Fatalf("partial: %#v", partial)
	}
	if partial.EnrouteTime != "0000" || partial.FuelTime != "0000" {
		t.Fatalf("partial times: %#v", partial)
	}
}

func TestDatafeedPilotJSONMarshal_EmbedShadowing(t *testing.T) {
	info := "I:B738:450:KLAX:1200:0000:FL350:KJFK:2:5:3:0::remarks:route"
	pilot := serviceapi.OnlineUserPilot{
		OnlineUserGeneralData: serviceapi.OnlineUserGeneralData{
			Callsign: "UAL1",
			CID:      100,
		},
		Altitude:           35000,
		PilotRating:        15,
		FlightPlan:         info,
		AssignedBeaconCode: "1200",
	}
	dp := DatafeedPilot{
		OnlineUserPilot: pilot,
		Server:          "TESTSRV",
		MilitaryRating:  0,
		QnhIHg:          0,
		QnhMb:           0,
		FlightPlan:      mapInfoSectionToDatafeedFP(pilot.FlightPlan, pilot.AssignedBeaconCode),
	}
	b, err := json.Marshal(dp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	// Real pilot_rating from embed (not outer zero).
	if pr, ok := m["pilot_rating"].(float64); !ok || pr != 15 {
		t.Fatalf("pilot_rating=%v want 15 (json=%s)", m["pilot_rating"], b)
	}
	// flight_plan must be an object, not a string and not omitted.
	fpObj, ok := m["flight_plan"].(map[string]any)
	if !ok {
		t.Fatalf("flight_plan type %T want object; json=%s", m["flight_plan"], b)
	}
	if fpObj["departure"] != "KLAX" || fpObj["arrival"] != "KJFK" {
		t.Fatalf("flight_plan airports: %#v", fpObj)
	}
	if fpObj["enroute_time"] != "0205" {
		t.Fatalf("enroute_time=%v want 0205", fpObj["enroute_time"])
	}
	if m["military_rating"].(float64) != 0 || m["qnh_i_hg"].(float64) != 0 || m["qnh_mb"].(float64) != 0 {
		t.Fatalf("expected honest zero QNH/military: %s", b)
	}
	if m["server"] != "TESTSRV" {
		t.Fatalf("server=%v", m["server"])
	}
}

func TestDatafeedATCJSON_NoOuterTextATISZero(t *testing.T) {
	atc := serviceapi.OnlineUserATC{
		OnlineUserGeneralData: serviceapi.OnlineUserGeneralData{Callsign: "LAX_TWR"},
		Frequency:             "118500",
		// TextATIS empty → omitempty on embed
	}
	da := DatafeedATC{OnlineUserATC: atc, Server: "S1"}
	b, err := json.Marshal(da)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, has := m["text_atis"]; has {
		t.Fatalf("empty text_atis should omit: %s", b)
	}
	// With embed ATIS present:
	atc.TextATIS = []string{"line1"}
	da = DatafeedATC{OnlineUserATC: atc, Server: "S1"}
	b, err = json.Marshal(da)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	atis, ok := m["text_atis"].([]any)
	if !ok || len(atis) != 1 || atis[0] != "line1" {
		t.Fatalf("text_atis from embed: %s", b)
	}
}
