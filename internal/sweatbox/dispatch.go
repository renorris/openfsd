package sweatbox

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// add usage string (TWRTrainer-themed).
const addUsage = `Missing parameters. Example: "add i h j 4r 15" or "add v s p @GA1" or "add i l j -270 15 2500"`

// Command dispatches an instructor text command.
//
// Targeting (TWRTrainer style):
//   - Global: add, p/pause, un/unpause, ops/stats (no aircraft)
//   - "CALLSIGN, verb args" embeds the target callsign
//   - selectedCS is used when the line has no embedded callsign and the verb
//     is aircraft-scoped (session-selected aircraft in the UI)
//
// Soft errors return OK=false with Message; they are not Go errors.
func (e *Engine) Command(selectedCS, line string) CommandResult {
	tokens := tokenizeCommand(line)
	if len(tokens) == 0 {
		return CommandResult{OK: false, Message: "Invalid command: empty"}
	}

	csFromLine, rest, _ := stripCallsignPrefix(tokens)
	if len(rest) == 0 {
		return CommandResult{OK: false, Message: "Invalid command: empty"}
	}

	verb := normalizeVerb(rest[0])
	args := rest[1:]

	// Resolve target for aircraft-scoped commands.
	target := strings.ToUpper(strings.TrimSpace(csFromLine))
	if target == "" {
		target = strings.ToUpper(strings.TrimSpace(selectedCS))
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	switch verb {
	case "add":
		return e.cmdAddLocked(args)
	case "pause":
		e.paused = true
		return CommandResult{OK: true}
	case "unpause":
		e.paused = false
		return CommandResult{OK: true}
	case "ops":
		return CommandResult{OK: true, Message: formatOpsMessage(e.opsLocked())}
	case "del":
		return e.cmdDelLocked(target)
	case "pos":
		return e.cmdPosLocked(target)
	case "sq":
		return e.cmdSquawkLocked(target, args, false)
	case "sqi":
		return e.cmdSquawkLocked(target, args, true)
	case "sn":
		return e.cmdXPDRLocked(target, XPDRModeNormal)
	case "ss":
		return e.cmdXPDRLocked(target, XPDRModeStandby)
	case "id":
		return e.cmdIdentLocked(target)
	default:
		return CommandResult{OK: false, Message: "Invalid command: " + rest[0]}
	}
}

// CommandLine is Command with no session-selected callsign.
func (e *Engine) CommandLine(line string) CommandResult {
	return e.Command("", line)
}

func (e *Engine) cmdDelLocked(target string) CommandResult {
	if target == "" {
		return CommandResult{OK: false, Message: "No aircraft selected."}
	}
	if !e.deleteLocked(target) {
		return CommandResult{OK: false, Message: fmt.Sprintf("Aircraft not found: %s", target)}
	}
	return CommandResult{OK: true, Deleted: []string{target}}
}

func (e *Engine) cmdPosLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	switch ac.Status {
	case StatusParked, StatusTaxiing, StatusHoldingShort, StatusHoldingInPosition, StatusLanded:
		// ok
	default:
		return CommandResult{OK: false, Message: "Not taxiing, parked, holding short, holding in position, or landed."}
	}
	ac.Status = StatusHoldingInPosition
	ac.PositionHold = true
	ac.Instruction = "Position and hold"
	return CommandResult{OK: true}
}

func (e *Engine) cmdSquawkLocked(target string, args []string, ident bool) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if len(args) < 1 {
		return CommandResult{OK: false, Message: "Missing parameters. Example: \"sq 1200\""}
	}
	code := strings.TrimSpace(args[0])
	if !isSquawk(code) {
		return CommandResult{OK: false, Message: "Invalid squawk code."}
	}
	ac.Squawk = code
	if ident {
		ac.Ident = true
		ac.XPDRMode = XPDRModeNormal
	}
	return CommandResult{OK: true}
}

func (e *Engine) cmdXPDRLocked(target, mode string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	ac.XPDRMode = mode
	// Standby and ident are contradictory on the wire; clear flash on ss.
	if mode == XPDRModeStandby {
		ac.Ident = false
	}
	return CommandResult{OK: true}
}

func (e *Engine) cmdIdentLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	ac.Ident = true
	ac.XPDRMode = XPDRModeNormal
	return CommandResult{OK: true}
}

func (e *Engine) requireAircraftLocked(target string) (*SimAircraft, string) {
	if target == "" {
		return nil, "No aircraft selected."
	}
	ac, ok := e.aircraft[target]
	if !ok {
		return nil, fmt.Sprintf("Aircraft not found: %s", target)
	}
	return ac, ""
}

// cmdAddLocked implements the three add forms:
//
//	add rules weight engine runway distance [type]
//	add rules weight engine @parking [type]
//	add rules weight engine -bearing distance altitude [type]
func (e *Engine) cmdAddLocked(args []string) CommandResult {
	if e.airport == nil {
		return CommandResult{OK: false, Message: "No airport loaded."}
	}
	if len(e.aircraft) >= e.settings.MaxAircraft {
		return CommandResult{OK: false, Message: fmt.Sprintf("Maximum aircraft (%d) reached.", e.settings.MaxAircraft)}
	}
	// Minimum: rules weight engine location...
	if len(args) < 4 {
		return CommandResult{OK: false, Message: addUsage}
	}

	rules := strings.ToUpper(args[0])
	weight := strings.ToUpper(args[1])
	engine := strings.ToUpper(args[2])
	loc := args[3]

	switch rules {
	case RulesVFR, RulesIFR, RulesDVFR, RulesSVFR:
		// ok (args already upper-cased)
	default:
		return CommandResult{OK: false, Message: addUsage}
	}
	switch weight {
	case WeightSmall, WeightSmallP, WeightLarge, WeightHeavy:
	default:
		return CommandResult{OK: false, Message: addUsage}
	}
	switch engine {
	case EnginePiston, EngineTurboprop, EngineJet, EngineHelicopter:
	default:
		return CommandResult{OK: false, Message: addUsage}
	}
	if !validWeightEngine(weight, engine) {
		return CommandResult{OK: false, Message: "Invalid combination of weight class and engine type."}
	}

	ac := &SimAircraft{
		Rules:    rules,
		Weight:   weight,
		Engine:   engine,
		XPDRMode: XPDRModeNormal,
	}

	// Branch on location form.
	switch {
	case strings.HasPrefix(loc, "@"):
		// Parking: add … @space [type]
		parkName := strings.ToUpper(strings.TrimSpace(loc[1:]))
		typeTok, errMsg := optionalType(args[4:])
		if errMsg != "" {
			return CommandResult{OK: false, Message: errMsg}
		}
		if errMsg = e.placeAtParkingLocked(ac, parkName); errMsg != "" {
			return CommandResult{OK: false, Message: errMsg}
		}
		ac.Type = resolveType(typeTok, weight, engine)

	case strings.HasPrefix(loc, "-"):
		// Bearing: add … -bearing distance altitude [type]
		if len(args) < 6 {
			return CommandResult{OK: false, Message: addUsage}
		}
		bearing, ok1 := parseFiniteFloat(loc[1:])
		distNM, ok2 := parseFiniteFloat(args[4])
		alt, ok3 := parseFiniteFloat(args[5])
		if !ok1 || !ok2 || !ok3 || distNM < 0 {
			return CommandResult{OK: false, Message: addUsage}
		}
		typeTok, errMsg := optionalType(args[6:])
		if errMsg != "" {
			return CommandResult{OK: false, Message: errMsg}
		}
		e.placeOnBearingLocked(ac, bearing, distNM, alt)
		ac.Type = resolveType(typeTok, weight, engine)

	default:
		// Approach: add … runway distance [type]
		if len(args) < 5 {
			return CommandResult{OK: false, Message: addUsage}
		}
		rwy := strings.ToUpper(loc)
		distNM, ok := parseFiniteFloat(args[4])
		if !ok || distNM < 0 {
			return CommandResult{OK: false, Message: addUsage}
		}
		typeTok, errMsg := optionalType(args[5:])
		if errMsg != "" {
			return CommandResult{OK: false, Message: errMsg}
		}
		if errMsg = e.placeOnApproachLocked(ac, rwy, distNM); errMsg != "" {
			return CommandResult{OK: false, Message: errMsg}
		}
		ac.Type = resolveType(typeTok, weight, engine)
	}

	ac.Callsign = e.generateCallsignLocked(engine)
	ac.Squawk = e.nextSquawkLocked(rules)
	ac.CruiseAlt = defaultCruiseAlt(engine)
	if ac.Speed == 0 && ac.Status != StatusParked {
		ac.Speed = defaultApproachSpeed(engine)
	}
	// Flight plan endpoints.
	icao := e.airport.ICAO
	if ac.Status == StatusParked {
		ac.Dep = icao
	} else {
		ac.Arr = icao
	}

	e.aircraft[ac.Callsign] = ac
	return CommandResult{OK: true, Added: []AircraftSnapshot{ac.snapshot()}}
}

// parseFiniteFloat parses a float that must be finite (rejects NaN/±Inf).
func parseFiniteFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func optionalType(args []string) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	if len(args) > 1 {
		// Multi-token types are rare; TWRTrainer allows "H60" after parking name
		// already consumed. Extra junk → usage error.
		return "", addUsage
	}
	t := strings.ToUpper(strings.TrimSpace(args[0]))
	if t == "" {
		return "", addUsage
	}
	return t, ""
}

func resolveType(override, weight, engine string) string {
	if override != "" {
		return override
	}
	return defaultType(weight, engine)
}

func (e *Engine) placeAtParkingLocked(ac *SimAircraft, parkName string) string {
	if parkName == "" {
		return "Unknown parking space."
	}
	var s *Surface
	if e.graph != nil {
		s = e.graph.Surface(parkName)
	}
	if s == nil && e.airport != nil {
		s = e.airport.FindSurface(parkName)
	}
	if s == nil || s.Kind != SurfaceParking {
		return "Unknown parking space."
	}
	if len(s.Points) == 0 {
		return "Unknown parking space."
	}
	pt := s.Points[0]
	ac.Lat = pt.Lat
	ac.Lon = pt.Lon
	ac.Alt = e.airport.FieldElev
	ac.Speed = 0
	ac.Heading = 0
	ac.Status = StatusParked
	ac.Instruction = "Parked"
	ac.Parking = strings.ToUpper(s.Name)
	ac.CurrentSurface = strings.ToUpper(s.Name)
	return ""
}

func (e *Engine) placeOnBearingLocked(ac *SimAircraft, bearing, distNM, alt float64) {
	ref := fieldReferencePoint(e.airport)
	lat, lon := destinationPoint(ref.Lat, ref.Lon, bearing, distNM*metersPerNM)
	ac.Lat = lat
	ac.Lon = lon
	ac.Alt = alt
	// Inbound toward field: reciprocal of radial.
	ac.Heading = normalizeHeading(bearing + 180)
	ac.Speed = defaultApproachSpeed(ac.Engine)
	ac.Status = StatusAirborne
	ac.Instruction = fmt.Sprintf("Inbound on the %03.0f radial", bearing)
}

func (e *Engine) placeOnApproachLocked(ac *SimAircraft, rwy string, distNM float64) string {
	var s *Surface
	if e.graph != nil {
		s = e.graph.Surface(rwy)
	}
	if s == nil && e.airport != nil {
		s = e.airport.FindSurface(rwy)
	}
	if s == nil || s.Kind != SurfaceRunway {
		return "Runway/taxiway not found in airport file."
	}
	thr, hdg, ok := runwayThreshold(s, rwy)
	if !ok {
		return "Runway/taxiway not found in airport file."
	}
	// Record the resolved end designator (combined "33/15" → RwyA).
	landEnd := rwy
	if rwy == s.Name || rwy == s.RwyA+"/"+s.RwyB {
		landEnd = s.RwyA
	}
	// Place along final: opposite of landing heading from threshold.
	finalBearing := normalizeHeading(hdg + 180)
	lat, lon := destinationPoint(thr.Lat, thr.Lon, finalBearing, distNM*metersPerNM)
	ac.Lat = lat
	ac.Lon = lon
	ac.Alt = approachAltitude(e.airport.FieldElev, distNM)
	ac.Heading = hdg
	ac.Speed = defaultApproachSpeed(ac.Engine)
	ac.Status = StatusOnApproach
	ac.LandingRunway = landEnd
	ac.Instruction = "Approach runway " + landEnd
	return ""
}
