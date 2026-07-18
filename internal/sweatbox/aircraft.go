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

	// Pattern legs (P1) — Status equals the active leg name while in the circuit.
	StatusUpwind    = "Upwind"
	StatusCrosswind = "Crosswind"
	StatusDownwind  = "Downwind"
	StatusBase      = "Base"
	StatusFinal     = "Final"
)

// Landing-type codes for pattern / approach clearances (tg/sg/la/fs).
const (
	LandingTG = "TG" // touch and go
	LandingSG = "SG" // stop and go
	LandingLA = "LA" // low approach
	LandingFS = "FS" // full stop
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

	// Ground / approach context.
	CurrentSurface string
	Parking        string
	LandingRunway  string
	DepRunway      string
	PositionHold   bool

	// Taxi path state (set by taxi/hold/res/cross; Tick consumes in PR 4).
	// Waypoints are a value copy of PlanTaxi output; WPIndex is the next
	// waypoint to fly toward (0 = start of path).
	TaxiWaypoints []Point
	TaxiWPIndex   int
	TaxiHolds     []TaxiHold // remaining hold-shorts along the path
	TaxiSteps     []string   // planned surface steps (canonical names)
	TaxiParking   string     // destination parking (no "@"), or empty

	// HoldShortOf is the surface name when StatusHoldingShort (or the next
	// planned hold when still taxiing). Cleared by res/cross/new taxi.
	HoldShortOf string

	// Takeoff clearance / departure (set by cto family; Tick consumes in PR 4).
	ClearedTakeoff bool
	DepHeading     float64 // used when HasDepHeading
	HasDepHeading  bool
	// PatternTraffic is "L" or "R" after ctomlt/ctomrt/mlt/mrt; empty otherwise.
	PatternTraffic string
	// NoStop is set by nostop/nohold (don't stop when clear of runway).
	NoStop bool

	// Pattern flying (P1). InPattern is true while Status is a pattern leg or
	// the aircraft is on a closed-traffic takeoff that will rejoin.
	InPattern bool
	// PatternSizeNM overrides airport PatternSize when ≥ 0.5; 0 → airport default.
	PatternSizeNM float64
	// LandingType is TG/SG/LA/FS (empty until cleared).
	LandingType string
	// ExtendLeg freezes leg advancement until tc/td/tb (or ga/re-enter).
	ExtendLeg bool
	// ShortApproach (msa): from downwind, fly direct to threshold (skip base).
	ShortApproach bool
	// MidfieldReported is set once when crossing midfield downwind (instruction).
	MidfieldReported bool
	// SGWaitSec is the stop-and-go hold duration; 0 means wait for `go` forever.
	// SGTimer counts remaining seconds while stopped on the runway for SG.
	SGWaitSec float64
	SGTimer   float64
	// SGWaiting is true while stopped mid-runway awaiting `go` / timer.
	SGWaiting bool

	// Air vector targets (set by fh/cm/spd family; Tick consumes in PR 4).
	// Has* flags distinguish "not commanded" from zero values.
	DesiredHeading    float64
	HasDesiredHeading bool
	// TurnDir: TurnShortest (0), TurnRight (+1), or TurnLeft (-1).
	TurnDir int
	// ImmediateHeading is set by fhn so the next tick (or domain apply) snaps
	// heading instead of turning. fh/tr/tl/fph clear it.
	ImmediateHeading bool

	DesiredAlt    float64
	HasDesiredAlt bool

	DesiredSpeed    float64
	HasDesiredSpeed bool
}

// Turn direction for heading vectors (consumed by PR 4 kinematics).
const (
	TurnShortest = 0
	TurnRight    = 1
	TurnLeft     = -1
)

// snapshot returns a value copy suitable for host / HTTP (no shared pointers).
func (a *SimAircraft) snapshot() AircraftSnapshot {
	if a == nil {
		return AircraftSnapshot{}
	}
	return AircraftSnapshot{
		Callsign:          a.Callsign,
		Type:              a.Type,
		Engine:            a.Engine,
		Rules:             a.Rules,
		Weight:            a.Weight,
		Dep:               a.Dep,
		Arr:               a.Arr,
		CruiseAlt:         a.CruiseAlt,
		Route:             a.Route,
		Remarks:           a.Remarks,
		Squawk:            a.Squawk,
		XPDRMode:          a.XPDRMode,
		Ident:             a.Ident,
		Lat:               a.Lat,
		Lon:               a.Lon,
		Alt:               a.Alt,
		Speed:             a.Speed,
		Heading:           a.Heading,
		Status:            a.Status,
		Instruction:       a.Instruction,
		CurrentSurface:    a.CurrentSurface,
		Parking:           a.Parking,
		LandingRunway:     a.LandingRunway,
		DepRunway:         a.DepRunway,
		PositionHold:      a.PositionHold,
		HoldShortOf:       a.HoldShortOf,
		ClearedTakeoff:    a.ClearedTakeoff,
		DepHeading:        a.DepHeading,
		HasDepHeading:     a.HasDepHeading,
		PatternTraffic:    a.PatternTraffic,
		NoStop:            a.NoStop,
		InPattern:         a.InPattern,
		PatternSizeNM:     a.PatternSizeNM,
		LandingType:       a.LandingType,
		ExtendLeg:         a.ExtendLeg,
		ShortApproach:     a.ShortApproach,
		SGWaitSec:         a.SGWaitSec,
		SGWaiting:         a.SGWaiting,
		TaxiWPIndex:       a.TaxiWPIndex,
		TaxiParking:       a.TaxiParking,
		TaxiSteps:         append([]string(nil), a.TaxiSteps...),
		DesiredHeading:    a.DesiredHeading,
		HasDesiredHeading: a.HasDesiredHeading,
		TurnDir:           a.TurnDir,
		ImmediateHeading:  a.ImmediateHeading,
		DesiredAlt:        a.DesiredAlt,
		HasDesiredAlt:     a.HasDesiredAlt,
		DesiredSpeed:      a.DesiredSpeed,
		HasDesiredSpeed:   a.HasDesiredSpeed,
	}
}

// inPatternLeg reports whether Status is one of the five circuit legs.
func (a *SimAircraft) inPatternLeg() bool {
	if a == nil {
		return false
	}
	switch a.Status {
	case StatusUpwind, StatusCrosswind, StatusDownwind, StatusBase, StatusFinal:
		return true
	default:
		return false
	}
}

// surfaceForTaxi is the surface name PlanTaxi should use as "current".
func (a *SimAircraft) surfaceForTaxi() string {
	if a == nil {
		return ""
	}
	if a.CurrentSurface != "" {
		return a.CurrentSurface
	}
	return a.Parking
}

// hasTaxiPath reports whether a ground route is loaded for motion.
func (a *SimAircraft) hasTaxiPath() bool {
	return a != nil && len(a.TaxiWaypoints) > 0
}

// groundOK reports whether the aircraft is in a ground status eligible for
// taxi / pos / hold-family commands (not airborne / approach / takeoff roll).
func (a *SimAircraft) groundOK() bool {
	if a == nil {
		return false
	}
	switch a.Status {
	case StatusParked, StatusTaxiing, StatusHoldingShort, StatusHoldingInPosition,
		StatusLanded, StatusHolding:
		return true
	default:
		return false
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
