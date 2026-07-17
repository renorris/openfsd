package sweatbox

import (
	"fmt"
	"math"
	"strings"
)

// Status strings aligned with TWRTrainer vocabulary where practical.
const (
	StatusParked            = "Parked"
	StatusTaxiing           = "Taxiing"
	StatusHoldingShort      = "Holding Short"
	StatusHoldingInPosition = "Holding in Position"
	StatusTakeoff           = "Takeoff"
	StatusDeparting         = "Departing"
	StatusOnApproach        = "On Approach"
	StatusAirborne          = "Airborne"
	StatusLanded            = "Landed"
	StatusHolding           = "Holding"
)

// Weight class codes (add command).
const (
	WeightSmall  = "S"
	WeightSmallP = "M" // small+ / medium
	WeightLarge  = "L"
	WeightHeavy  = "H"
)

// SimAircraft is live per-aircraft simulation state inside the Engine.
// Domain-only: no protocol / session types.
type SimAircraft struct {
	Callsign string
	Type     string // ICAO type, optional equipment suffix
	Engine   string // P, T, J, H
	Rules    string // V, I, D, S
	Weight   string // S, M, L, H

	Dep       string
	Arr       string
	CruiseAlt int
	Route     string
	Remarks   string

	Squawk   string
	XPDRMode string // N or S
	Ident    bool   // true after id / sqi until cleared by host/tick later

	Lat     float64
	Lon     float64
	Alt     float64 // feet MSL
	Speed   float64 // knots
	Heading float64 // degrees true-ish (sim domain; mag later if needed)

	Status      string
	Instruction string

	// Ground / approach context (used by later taxi/pattern PRs).
	CurrentSurface string
	Parking        string
	LandingRunway  string
	DepRunway      string
	PositionHold   bool
}

// snapshot returns a value copy suitable for host / HTTP (no shared pointers).
func (a *SimAircraft) snapshot() AircraftSnapshot {
	if a == nil {
		return AircraftSnapshot{}
	}
	return AircraftSnapshot{
		Callsign:       a.Callsign,
		Type:           a.Type,
		Engine:         a.Engine,
		Rules:          a.Rules,
		Weight:         a.Weight,
		Dep:            a.Dep,
		Arr:            a.Arr,
		CruiseAlt:      a.CruiseAlt,
		Route:          a.Route,
		Remarks:        a.Remarks,
		Squawk:         a.Squawk,
		XPDRMode:       a.XPDRMode,
		Ident:          a.Ident,
		Lat:            a.Lat,
		Lon:            a.Lon,
		Alt:            a.Alt,
		Speed:          a.Speed,
		Heading:        a.Heading,
		Status:         a.Status,
		Instruction:    a.Instruction,
		CurrentSurface: a.CurrentSurface,
		Parking:        a.Parking,
		LandingRunway:  a.LandingRunway,
		DepRunway:      a.DepRunway,
		PositionHold:   a.PositionHold,
	}
}

// fromScenarioRow builds a SimAircraft from a parsed .air row.
func fromScenarioRow(row Aircraft) *SimAircraft {
	status := StatusAirborne
	if row.Speed <= 0 {
		status = StatusParked
	}
	return &SimAircraft{
		Callsign:  strings.ToUpper(strings.TrimSpace(row.Callsign)),
		Type:      row.Type,
		Engine:    row.Engine,
		Rules:     row.Rules,
		Weight:    inferWeight(row.Engine, row.Type),
		Dep:       row.Dep,
		Arr:       row.Arr,
		CruiseAlt: row.CruiseAlt,
		Route:     row.Route,
		Remarks:   row.Remarks,
		Squawk:    row.Squawk,
		XPDRMode:  row.XPDRMode,
		Lat:       row.Lat,
		Lon:       row.Lon,
		Alt:       row.Alt,
		Speed:     row.Speed,
		Heading:   normalizeHeading(row.Heading),
		Status:    status,
	}
}

// inferWeight picks a plausible weight class when loading .air (no weight field).
func inferWeight(engine, acType string) string {
	t := strings.ToUpper(acType)
	switch {
	case strings.HasPrefix(t, "B74"), strings.HasPrefix(t, "B77"), strings.HasPrefix(t, "B78"),
		strings.HasPrefix(t, "A38"), strings.HasPrefix(t, "A34"), strings.HasPrefix(t, "MD1"):
		return WeightHeavy
	case strings.HasPrefix(t, "B73"), strings.HasPrefix(t, "B75"), strings.HasPrefix(t, "B76"),
		strings.HasPrefix(t, "A31"), strings.HasPrefix(t, "A32"), strings.HasPrefix(t, "A33"):
		return WeightLarge
	case engine == EngineHelicopter:
		return WeightSmall
	case engine == EnginePiston:
		return WeightSmall
	case engine == EngineTurboprop:
		return WeightSmallP
	default:
		return WeightLarge
	}
}

// defaultType returns a default ICAO type for weight+engine when the instructor
// omits an override. Empty if the combination is invalid.
func defaultType(weight, engine string) string {
	w := strings.ToUpper(weight)
	e := strings.ToUpper(engine)
	switch e {
	case EngineHelicopter:
		switch w {
		case WeightSmall:
			return "R22"
		case WeightSmallP:
			return "B06"
		default:
			return ""
		}
	case EnginePiston:
		switch w {
		case WeightSmall:
			return "C172"
		case WeightSmallP:
			return "BE58"
		default:
			return ""
		}
	case EngineTurboprop:
		switch w {
		case WeightSmall:
			return "PC12"
		case WeightSmallP:
			return "DH8D"
		case WeightLarge:
			return "AT72"
		default:
			return ""
		}
	case EngineJet:
		switch w {
		case WeightSmall:
			return "C510"
		case WeightSmallP:
			return "CRJ2"
		case WeightLarge:
			return "B738"
		case WeightHeavy:
			return "B744"
		default:
			return ""
		}
	}
	return ""
}

// validWeightEngine reports whether the weight/engine pair is allowed.
func validWeightEngine(weight, engine string) bool {
	return defaultType(weight, engine) != ""
}

// defaultApproachSpeed knots by engine class.
func defaultApproachSpeed(engine string) float64 {
	switch strings.ToUpper(engine) {
	case EnginePiston:
		return 90
	case EngineTurboprop:
		return 140
	case EngineJet:
		return 180
	case EngineHelicopter:
		return 80
	default:
		return 120
	}
}

// defaultCruiseAlt feet by engine class (filed plan placeholder).
func defaultCruiseAlt(engine string) int {
	switch strings.ToUpper(engine) {
	case EnginePiston, EngineHelicopter:
		return 3500
	case EngineTurboprop:
		return 16000
	case EngineJet:
		return 29000
	default:
		return 10000
	}
}

// normalizeHeading wraps heading into [0, 360).
func normalizeHeading(h float64) float64 {
	if math.IsNaN(h) || math.IsInf(h, 0) {
		return 0
	}
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	// Avoid -0.
	if h == 0 {
		return 0
	}
	return h
}

// formatCallsignError is a small helper for stable messages.
func formatCallsignInUse(cs string) string {
	return fmt.Sprintf("Callsign already in use: %s", cs)
}
