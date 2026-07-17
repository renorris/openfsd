package server

import (
	"strconv"
	"strings"

	"github.com/renorris/openfsd/internal/sweatbox"
)

// defaultCruiseTAS is used when aircraft speed/type does not imply a better TAS.
const defaultCruiseTAS = 250

// encodeFlightPlanInfo builds the session FlightPlan info section (no $FP
// source/dest prefix). Field order matches docs/protocol.md Flight Plan and
// extractFlightplanInfoSection / buildFileFlightplanPacket consumers.
//
//	rules:type:tas:dep:etd:atd:cruise:dest:hre:mre:hfuel:mfuel:altn:remarks:route
func encodeFlightPlanInfo(ac sweatbox.AircraftSnapshot) string {
	rules := strings.ToUpper(strings.TrimSpace(ac.Rules))
	if rules == "" {
		rules = sweatbox.RulesIFR
	}
	typ := strings.TrimSpace(ac.Type)
	if typ == "" {
		typ = "ZZZZ"
	}
	tas := defaultCruiseTAS
	if ac.Speed > 0 {
		// Prefer current GS as a stand-in when airborne; still default for parked.
		if ac.Speed >= 50 {
			tas = int(ac.Speed + 0.5)
		}
	}
	dep := strings.ToUpper(strings.TrimSpace(ac.Dep))
	arr := strings.ToUpper(strings.TrimSpace(ac.Arr))
	cruise := strconv.Itoa(ac.CruiseAlt)
	remarks := ac.Remarks
	route := ac.Route

	// Fields 0–14; empty alternate yields "::" before remarks.
	parts := []string{
		rules,             // 0 flight rules
		typ,               // 1 equipment / type
		strconv.Itoa(tas), // 2 true airspeed
		dep,               // 3 departure
		"0000",            // 4 ETD
		"0000",            // 5 actual dep time
		cruise,            // 6 cruise altitude
		arr,               // 7 destination
		"0",               // 8 hours enroute
		"0",               // 9 minutes enroute
		"0",               // 10 hours fuel
		"0",               // 11 minutes fuel
		"",                // 12 alternate (unused in v1)
		remarks,           // 13 remarks
		route,             // 14 route
	}
	return strings.Join(parts, ":")
}
