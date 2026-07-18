package sweatbox

import (
	"math"
	"sort"

	"github.com/renorris/openfsd/internal/geo"
)

// Simple kinematics constants (not a full flight model).
// Tuned for readable instructor-time motion at ~1 Hz ticks.
const (
	// Ground taxi speed (knots).
	taxiSpeedKt = 15.0

	// Airborne standard-rate-ish turn (°/s).
	turnRateDegPerSec = 3.0

	// Default climb / descent rate (ft/min) for vectors.
	climbRateFpm = 1500.0

	// Takeoff initial-climb rate (ft/min); steeper than enroute vectors.
	takeoffClimbRateFpm = 2500.0

	// Pattern climb / descent (ft/min) — gentler for circuit work.
	patternClimbRateFpm = 1000.0

	// Airspeed change rate when DesiredSpeed is set (kt/s).
	speedChangeKtPerSec = 8.0

	// Takeoff roll acceleration (kt/s).
	takeoffAccelKtPerSec = 6.0

	// Snap distance when arriving at a taxi waypoint (meters).
	taxiArriveEpsM = 3.0

	// Consider altitudes equal within this many feet.
	altEqualEpsFt = 5.0

	// Consider headings equal within this many degrees.
	hdgEqualEpsDeg = 0.5

	// Consider speeds equal within this many knots.
	spdEqualEpsKt = 0.5
)

// knotsToMps converts knots to meters per second.
func knotsToMps(kt float64) float64 {
	return kt * metersPerNM / 3600.0
}

// rotateSpeedKt is the approximate rotate / lift-off speed by engine class.
func rotateSpeedKt(engine string) float64 {
	switch engine {
	case EngineHelicopter:
		return 20
	case EnginePiston:
		return 55
	case EngineTurboprop:
		return 90
	case EngineJet:
		return 140
	default:
		return 80
	}
}

// initialClimbTarget returns the MSL altitude to level at after takeoff.
func initialClimbTarget(apt *Airport, engine string) float64 {
	if apt == nil {
		return 3000
	}
	var target float64
	switch engine {
	case EngineJet:
		target = apt.InitClimbJets
	default:
		target = apt.InitClimbProps
	}
	if target <= 0 {
		target = 3000
	}
	// If configured below field elevation, treat as AGL offset.
	if target < apt.FieldElev {
		return apt.FieldElev + target
	}
	return target
}

// fieldElevFeet returns airport field elevation or 0.
func fieldElevFeet(apt *Airport) float64 {
	if apt == nil {
		return 0
	}
	return apt.FieldElev
}

// tickAircraftLocked advances one aircraft by dtSec seconds.
// Returns (changed, shouldDelete). Caller must hold e.mu.
func (e *Engine) tickAircraftLocked(ac *SimAircraft, dtSec float64) (changed bool, shouldDelete bool) {
	if ac == nil || dtSec <= 0 {
		return false, false
	}

	// Transition into takeoff when cleared and already at the runway.
	if ac.ClearedTakeoff {
		switch ac.Status {
		case StatusHoldingInPosition:
			ac.Status = StatusTakeoff
			ac.PositionHold = false
			ac.HoldShortOf = ""
			e.alignTakeoffHeadingLocked(ac)
			changed = true
		case StatusHoldingShort:
			if ac.DepRunway != "" && ac.HoldShortOf != "" &&
				e.sameSurfaceNameLocked(ac.HoldShortOf, ac.DepRunway) {
				ac.Status = StatusTakeoff
				ac.HoldShortOf = ""
				ac.PositionHold = false
				e.alignTakeoffHeadingLocked(ac)
				changed = true
			}
		}
	}

	switch ac.Status {
	case StatusParked:
		// Frozen at parking.
		return changed, false

	case StatusHolding, StatusHoldingShort, StatusHoldingInPosition:
		// Present-position hold, hold-short, or LUAW — no ground advance.
		if ac.Speed != 0 {
			ac.Speed = 0
			changed = true
		}
		return changed, false

	case StatusTaxiing:
		c, del := e.tickTaxiLocked(ac, dtSec)
		return changed || c, del

	case StatusTakeoff:
		c := e.tickTakeoffLocked(ac, dtSec)
		return changed || c, false

	case StatusDeparting, StatusAirborne, StatusOnApproach:
		c := e.tickAirborneLocked(ac, dtSec)
		return changed || c, false

	case StatusUpwind, StatusCrosswind, StatusDownwind, StatusBase, StatusFinal:
		c := e.tickPatternLocked(ac, dtSec)
		return changed || c, false

	case StatusLanded:
		// Stop-and-go waiting on the runway.
		if ac.SGWaiting {
			c := e.tickSGWaitLocked(ac, dtSec)
			return changed || c, false
		}
		// Landed roll without a taxi path: slow to a stop.
		if ac.hasTaxiPath() {
			c, del := e.tickTaxiLocked(ac, dtSec)
			return changed || c, del
		}
		if ac.Speed > spdEqualEpsKt {
			ac.Speed = math.Max(0, ac.Speed-speedChangeKtPerSec*dtSec)
			if ac.Speed < spdEqualEpsKt {
				ac.Speed = 0
			}
			// Roll along heading while decelerating.
			if ac.Speed > 0 {
				dist := knotsToMps(ac.Speed) * dtSec
				ac.Lat, ac.Lon = destinationPoint(ac.Lat, ac.Lon, ac.Heading, dist)
			}
			return true, false
		}
		return changed, false

	default:
		// Unknown status: still honor air vectors if moving.
		if ac.Speed > 0 || ac.HasDesiredHeading || ac.HasDesiredAlt || ac.HasDesiredSpeed {
			c := e.tickAirborneLocked(ac, dtSec)
			return changed || c, false
		}
		return changed, false
	}
}

// tickTaxiLocked advances along TaxiWaypoints at taxi speed.
// Stops at active hold-shorts; finishes path to parking / dep runway / stop.
// Always follows the polyline; holds only stop when the current waypoint index
// is the hold's WaypointIndex (no corner-cut to a future hold point).
func (e *Engine) tickTaxiLocked(ac *SimAircraft, dtSec float64) (changed bool, shouldDelete bool) {
	if !ac.hasTaxiPath() {
		if ac.Speed != 0 {
			ac.Speed = 0
			return true, false
		}
		return false, false
	}

	// Budget distance for this tick.
	ac.Speed = taxiSpeedKt
	budget := knotsToMps(taxiSpeedKt) * dtSec
	moved := false

	for budget > 0 {
		if ac.TaxiWPIndex >= len(ac.TaxiWaypoints) {
			return e.finishTaxiPathLocked(ac)
		}

		target := ac.TaxiWaypoints[ac.TaxiWPIndex]
		// Hold-short only when we have reached the hold's waypoint index.
		// Intermediate WPs are walked normally even if a later hold exists.
		h := holdAtIndex(ac, ac.TaxiWPIndex)

		d := geo.Distance(ac.Lat, ac.Lon, target.Lat, target.Lon)
		if d <= taxiArriveEpsM {
			// Arrive at current waypoint.
			ac.Lat, ac.Lon = target.Lat, target.Lon
			moved = true
			if h != nil {
				// Stop holding short at this waypoint; advance index for resume.
				ac.TaxiWPIndex++
				ac.Status = StatusHoldingShort
				ac.HoldShortOf = h.Name
				ac.Speed = 0
				ac.Instruction = "Holding short of " + h.Name
				return true, false
			}
			ac.TaxiWPIndex++
			if ac.TaxiWPIndex >= len(ac.TaxiWaypoints) {
				return e.finishTaxiPathLocked(ac)
			}
			continue
		}
		if d <= budget {
			// Consume full segment.
			brg := initialBearingDeg(ac.Lat, ac.Lon, target.Lat, target.Lon)
			ac.Heading = brg
			ac.Lat, ac.Lon = target.Lat, target.Lon
			budget -= d
			moved = true
			if h != nil {
				ac.TaxiWPIndex++
				ac.Status = StatusHoldingShort
				ac.HoldShortOf = h.Name
				ac.Speed = 0
				ac.Instruction = "Holding short of " + h.Name
				return true, false
			}
			ac.TaxiWPIndex++
			if ac.TaxiWPIndex >= len(ac.TaxiWaypoints) {
				return e.finishTaxiPathLocked(ac)
			}
			continue
		}
		// Partial segment toward current waypoint (never skip ahead to a hold).
		brg := initialBearingDeg(ac.Lat, ac.Lon, target.Lat, target.Lon)
		ac.Heading = brg
		ac.Lat, ac.Lon = destinationPoint(ac.Lat, ac.Lon, brg, budget)
		moved = true
		budget = 0
	}

	if moved {
		ac.Status = StatusTaxiing
		return true, false
	}
	return false, false
}

// holdAtIndex returns a remaining hold whose WaypointIndex equals wpIndex.
func holdAtIndex(ac *SimAircraft, wpIndex int) *TaxiHold {
	if ac == nil || len(ac.TaxiHolds) == 0 {
		return nil
	}
	for i := range ac.TaxiHolds {
		h := &ac.TaxiHolds[i]
		if h.WaypointIndex == wpIndex {
			return h
		}
	}
	return nil
}

// alignTakeoffHeadingLocked sets Heading to the dep runway landing/takeoff
// direction when DepRunway resolves. No-op if airport/runway unavailable.
func (e *Engine) alignTakeoffHeadingLocked(ac *SimAircraft) {
	if ac == nil || ac.DepRunway == "" || e.airport == nil {
		return
	}
	s := e.airport.FindSurface(ac.DepRunway)
	if s == nil {
		return
	}
	if _, hdg, ok := runwayThreshold(s, ac.DepRunway); ok {
		ac.Heading = hdg
	}
}

// finishTaxiPathLocked is called when the aircraft reaches the last taxi waypoint.
func (e *Engine) finishTaxiPathLocked(ac *SimAircraft) (changed bool, shouldDelete bool) {
	ac.Speed = 0
	ac.TaxiWaypoints = nil
	ac.TaxiWPIndex = 0
	ac.TaxiHolds = nil
	ac.TaxiSteps = nil

	// Parking destination.
	if ac.TaxiParking != "" {
		park := ac.TaxiParking
		ac.TaxiParking = ""
		ac.Parking = park
		ac.CurrentSurface = park
		ac.Status = StatusParked
		ac.Instruction = "Parked " + park
		ac.HoldShortOf = ""
		ac.PositionHold = false
		if e.settings.DeleteArrivalsWhenParked {
			return true, true
		}
		return true, false
	}

	// Cleared takeoff at end of route to departure runway.
	if ac.ClearedTakeoff && ac.DepRunway != "" {
		ac.Status = StatusTakeoff
		ac.HoldShortOf = ""
		ac.PositionHold = false
		ac.CurrentSurface = ac.DepRunway
		e.alignTakeoffHeadingLocked(ac)
		return true, false
	}

	// Destination runway without takeoff clearance → hold short.
	if ac.DepRunway != "" {
		ac.Status = StatusHoldingShort
		ac.HoldShortOf = ac.DepRunway
		ac.CurrentSurface = ac.DepRunway
		ac.Instruction = "Holding short of " + ac.DepRunway
		return true, false
	}

	// Taxiway-only end: stop taxiing in place.
	ac.Status = StatusTaxiing
	ac.Instruction = "Taxi"
	return true, false
}

// tickTakeoffLocked accelerates, rotates, climbs to initial climb, then departs.
// Closed-traffic takeoffs (PatternTraffic set) climb to pattern altitude and
// enter Upwind instead of Departing.
func (e *Engine) tickTakeoffLocked(ac *SimAircraft, dtSec float64) bool {
	apt := e.airport
	field := fieldElevFeet(apt)
	closed := ac.PatternTraffic == "L" || ac.PatternTraffic == "R"
	var targetAlt float64
	if closed {
		targetAlt = patternAltitudeMSL(apt)
		// Ensure we climb at least a few hundred feet AGL.
		if targetAlt < field+500 {
			targetAlt = field + 500
		}
	} else {
		targetAlt = initialClimbTarget(apt, ac.Engine)
	}
	vr := rotateSpeedKt(ac.Engine)
	changed := false

	// Ground roll: accelerate along heading.
	airborne := ac.Alt > field+altEqualEpsFt || ac.Speed >= vr
	if !airborne {
		prev := ac.Speed
		ac.Speed = math.Min(vr+10, ac.Speed+takeoffAccelKtPerSec*dtSec)
		if ac.Speed != prev {
			changed = true
		}
		// Align with dep heading once rolling if set (optional early turn not applied on ground).
		if ac.HasDepHeading && ac.Speed > vr*0.5 {
			// keep runway heading until rotate
		}
		dist := knotsToMps(ac.Speed) * dtSec
		if dist > 0 {
			ac.Lat, ac.Lon = destinationPoint(ac.Lat, ac.Lon, ac.Heading, dist)
			changed = true
		}
		// Rotate when at Vr.
		if ac.Speed >= vr {
			ac.Alt = field + 1
			changed = true
		}
		return changed
	}

	// Airborne portion of takeoff / initial climb.
	ac.Status = StatusTakeoff
	// Accelerate toward a modest climb speed (Vr + margin).
	climbSpd := vr + 30
	if ac.Speed < climbSpd {
		ac.Speed = math.Min(climbSpd, ac.Speed+takeoffAccelKtPerSec*dtSec)
		changed = true
	}

	// Climb toward target altitude.
	if ac.Alt < targetAlt-altEqualEpsFt {
		rate := takeoffClimbRateFpm
		if closed {
			rate = patternClimbRateFpm
		}
		delta := rate * dtSec / 60.0
		ac.Alt = math.Min(targetAlt, ac.Alt+delta)
		changed = true
	}

	// Closed traffic: stay runway heading until pattern entry.
	// Open departure: turn to departure heading once airborne.
	if !closed && ac.HasDepHeading {
		if turnToward(ac, ac.DepHeading, TurnShortest, dtSec) {
			changed = true
		}
	}

	// Advance position.
	if ac.Speed > 0 {
		dist := knotsToMps(ac.Speed) * dtSec
		ac.Lat, ac.Lon = destinationPoint(ac.Lat, ac.Lon, ac.Heading, dist)
		changed = true
	}

	// Level at climb target → pattern Upwind or Departing.
	if ac.Alt >= targetAlt-altEqualEpsFt {
		ac.Alt = targetAlt
		ac.ClearedTakeoff = false
		if closed {
			e.enterPatternFromTakeoffLocked(ac)
			changed = true
			return changed
		}
		ac.Status = StatusDeparting
		// Seed desired altitude so subsequent cm can override; until then hold.
		if !ac.HasDesiredAlt {
			ac.DesiredAlt = targetAlt
			ac.HasDesiredAlt = true
		}
		if ac.HasDepHeading && !ac.HasDesiredHeading {
			ac.DesiredHeading = ac.DepHeading
			ac.HasDesiredHeading = true
			ac.TurnDir = TurnShortest
		}
		ac.Instruction = "Departing"
		changed = true
	}
	return changed
}

// tickAirborneLocked applies air vector targets and integrates position.
func (e *Engine) tickAirborneLocked(ac *SimAircraft, dtSec float64) bool {
	changed := false

	// Heading
	if ac.HasDesiredHeading {
		if ac.ImmediateHeading {
			// fhn already snapped Heading in dispatch; clear the one-shot flag.
			if math.Abs(headingDelta(ac.Heading, ac.DesiredHeading)) > hdgEqualEpsDeg {
				ac.Heading = ac.DesiredHeading
				changed = true
			}
			ac.ImmediateHeading = false
			changed = true
		} else if turnToward(ac, ac.DesiredHeading, ac.TurnDir, dtSec) {
			changed = true
		}
	}

	// Altitude
	if ac.HasDesiredAlt {
		if climbToward(ac, ac.DesiredAlt, dtSec) {
			changed = true
		}
	}

	// Speed
	if ac.HasDesiredSpeed {
		if speedToward(ac, ac.DesiredSpeed, dtSec) {
			changed = true
		}
	}

	// Position along current heading (simple GS ≈ IAS).
	if ac.Speed > spdEqualEpsKt {
		dist := knotsToMps(ac.Speed) * dtSec
		ac.Lat, ac.Lon = destinationPoint(ac.Lat, ac.Lon, ac.Heading, dist)
		changed = true
	}

	// Promote takeoff→departing leftovers: if still "On Approach" keep status;
	// departing stays until instructor reclassifies.
	if ac.Status == StatusDeparting && ac.HasDesiredAlt &&
		math.Abs(ac.Alt-ac.DesiredAlt) <= altEqualEpsFt &&
		(!ac.HasDesiredHeading || math.Abs(headingDelta(ac.Heading, ac.DesiredHeading)) <= hdgEqualEpsDeg) {
		// Level and on heading — still Departing is fine for instruction UI.
	}

	return changed
}

// turnToward rotates ac.Heading toward target using turnDir.
// TurnShortest picks the smaller arc; TurnRight/TurnLeft force direction.
// Returns true if heading changed.
func turnToward(ac *SimAircraft, target float64, turnDir int, dtSec float64) bool {
	target = normalizeHeading(target)
	cur := normalizeHeading(ac.Heading)
	delta := headingDelta(cur, target) // signed shortest in (-180, 180]
	if math.Abs(delta) <= hdgEqualEpsDeg {
		if cur != target {
			ac.Heading = target
			return true
		}
		return false
	}

	step := turnRateDegPerSec * dtSec
	var move float64
	switch turnDir {
	case TurnRight:
		// Always turn right (positive) to target.
		need := delta
		if need < 0 {
			need += 360
		}
		if step >= need {
			ac.Heading = target
			return true
		}
		move = step
	case TurnLeft:
		// Always turn left (negative) to target.
		need := -delta
		if need < 0 {
			need += 360
		}
		if step >= need {
			ac.Heading = target
			return true
		}
		move = -step
	default:
		// Shortest.
		if step >= math.Abs(delta) {
			ac.Heading = target
			return true
		}
		if delta > 0 {
			move = step
		} else {
			move = -step
		}
	}
	ac.Heading = normalizeHeading(cur + move)
	return true
}

// headingDelta returns signed shortest delta from cur to target in (-180, 180].
func headingDelta(cur, target float64) float64 {
	d := normalizeHeading(target) - normalizeHeading(cur)
	for d > 180 {
		d -= 360
	}
	for d <= -180 {
		d += 360
	}
	return d
}

// climbToward adjusts altitude toward target at climbRateFpm.
func climbToward(ac *SimAircraft, target float64, dtSec float64) bool {
	diff := target - ac.Alt
	if math.Abs(diff) <= altEqualEpsFt {
		if ac.Alt != target {
			ac.Alt = target
			return true
		}
		return false
	}
	step := climbRateFpm * dtSec / 60.0
	if math.Abs(diff) <= step {
		ac.Alt = target
		return true
	}
	if diff > 0 {
		ac.Alt += step
	} else {
		ac.Alt -= step
	}
	return true
}

// speedToward adjusts IAS toward target.
func speedToward(ac *SimAircraft, target float64, dtSec float64) bool {
	if target < 0 {
		target = 0
	}
	diff := target - ac.Speed
	if math.Abs(diff) <= spdEqualEpsKt {
		if ac.Speed != target {
			ac.Speed = target
			return true
		}
		return false
	}
	step := speedChangeKtPerSec * dtSec
	if math.Abs(diff) <= step {
		ac.Speed = target
		return true
	}
	if diff > 0 {
		ac.Speed += step
	} else {
		ac.Speed -= step
	}
	return true
}

// tickPatternLocked advances one aircraft along the traffic pattern.
func (e *Engine) tickPatternLocked(ac *SimAircraft, dtSec float64) bool {
	if ac == nil || dtSec <= 0 {
		return false
	}
	a, errMsg := e.anchorsForAircraftLocked(ac)
	if errMsg != "" {
		// No runway geometry — fall back to free vectors.
		return e.tickAirborneLocked(ac, dtSec)
	}
	changed := false
	field := fieldElevFeet(e.airport)
	patAlt := patternAltitudeMSL(e.airport)
	leg := ac.Status

	// --- Altitude ---
	var altTarget float64
	switch leg {
	case StatusFinal:
		distNM := distToPoint(ac, a.Threshold) / metersPerNM
		if ac.LandingType == LandingLA {
			// Descend to field+200, level for overfly.
			altTarget = field + lowApproachAGLFt
			gs := approachAltitude(field, distNM)
			if gs > altTarget {
				altTarget = gs
			}
			// Once near/over runway, hold low-approach AGL.
			if distNM < 0.15 {
				altTarget = field + lowApproachAGLFt
			}
		} else {
			altTarget = approachAltitude(field, distNM)
		}
	default:
		altTarget = patAlt
	}
	ac.DesiredAlt = altTarget
	ac.HasDesiredAlt = true
	if climbTowardRate(ac, altTarget, patternClimbRateFpm, dtSec) {
		changed = true
	}

	// --- Speed ---
	if !ac.HasDesiredSpeed || ac.DesiredSpeed <= 0 {
		ac.DesiredSpeed = defaultApproachSpeed(ac.Engine)
		ac.HasDesiredSpeed = true
	}
	if speedToward(ac, ac.DesiredSpeed, dtSec) {
		changed = true
	}

	// --- Heading / track ---
	// Prefer flying toward the leg corner (or threshold on final) so early turns
	// from enter-* placement converge; fall back to nominal leg heading when
	// nearly on top of the target or extending.
	target := a.legTarget(leg)
	nomHdg := a.legHeading(leg)
	if ac.ExtendLeg {
		ac.DesiredHeading = nomHdg
		ac.HasDesiredHeading = true
		ac.TurnDir = patternTurnDir(a.Traffic)
	} else {
		d := distToPoint(ac, target)
		if d > patternCornerEpsM*0.5 {
			brg := initialBearingDeg(ac.Lat, ac.Lon, target.Lat, target.Lon)
			ac.DesiredHeading = brg
		} else {
			ac.DesiredHeading = nomHdg
		}
		ac.HasDesiredHeading = true
		ac.TurnDir = patternTurnDir(a.Traffic)
	}
	if turnToward(ac, ac.DesiredHeading, ac.TurnDir, dtSec) {
		changed = true
	}

	// --- Position ---
	if ac.Speed > spdEqualEpsKt {
		dist := knotsToMps(ac.Speed) * dtSec
		ac.Lat, ac.Lon = destinationPoint(ac.Lat, ac.Lon, ac.Heading, dist)
		changed = true
	}

	// --- Midfield downwind report ---
	if leg == StatusDownwind && !ac.MidfieldReported {
		if distToPoint(ac, a.MidfieldDW) <= midfieldEpsM {
			ac.MidfieldReported = true
			ac.Instruction = "Midfield on the downwind, runway " + a.Runway
			changed = true
		}
	}

	// --- Short approach: from downwind, pull toward threshold once past midfield ---
	if ac.ShortApproach && leg == StatusDownwind && !ac.ExtendLeg {
		// When roughly abeam / past midfield, cut to final.
		if ac.MidfieldReported || distToPoint(ac, a.MidfieldDW) <= midfieldEpsM {
			e.advancePatternLegLocked(ac, a) // forces Final via ShortApproach branch
			changed = true
			return changed
		}
	}

	// --- Final: landing / low approach / overshoot ---
	if leg == StatusFinal {
		dThr := distToPoint(ac, a.Threshold)
		// Along-track: if we've passed the threshold (beyond landing heading), treat as arrival.
		brgToThr := initialBearingDeg(ac.Lat, ac.Lon, a.Threshold.Lat, a.Threshold.Lon)
		past := math.Abs(headingDelta(brgToThr, a.LandingHdg)) > 90 && dThr < a.SizeNM*metersPerNM
		near := dThr <= patternThreshEpsM
		if near || past {
			c := e.handlePatternThresholdLocked(ac, a, dtSec)
			return changed || c
		}
		// Keep instruction current on final.
		want := formatPatternInstruction(ac)
		if ac.Instruction != want {
			ac.Instruction = want
			changed = true
		}
		return changed
	}

	// --- Leg advance at corners (unless extending) ---
	if !ac.ExtendLeg {
		d := distToPoint(ac, target)
		if d <= patternCornerEpsM {
			e.advancePatternLegLocked(ac, a)
			changed = true
		}
	}

	return changed
}

// handlePatternThresholdLocked resolves TG/SG/LA/FS when reaching the threshold.
func (e *Engine) handlePatternThresholdLocked(ac *SimAircraft, a PatternAnchors, dtSec float64) bool {
	field := fieldElevFeet(e.airport)
	lt := ac.LandingType
	if lt == "" {
		// Arrivals without an explicit type full-stop by default.
		if ac.InPattern {
			lt = LandingTG
		} else {
			lt = LandingFS
		}
	}
	changed := true

	switch lt {
	case LandingLA:
		// Overfly at low-approach AGL and continue upwind.
		ac.Alt = field + lowApproachAGLFt
		ac.Lat, ac.Lon = a.Threshold.Lat, a.Threshold.Lon
		ac.Heading = a.LandingHdg
		ac.Status = StatusUpwind
		ac.InPattern = true
		ac.DesiredHeading = a.legHeading(StatusUpwind)
		ac.HasDesiredHeading = true
		ac.TurnDir = patternTurnDir(a.Traffic)
		ac.DesiredAlt = patternAltitudeMSL(e.airport)
		ac.HasDesiredAlt = true
		ac.Instruction = "Low approach, " + formatPatternInstruction(ac)
		return changed

	case LandingTG:
		// Touch and go: put on runway, accelerate via takeoff → upwind.
		ac.Alt = field
		ac.Lat, ac.Lon = a.Threshold.Lat, a.Threshold.Lon
		ac.Heading = a.LandingHdg
		ac.Speed = math.Max(ac.Speed*0.6, rotateSpeedKt(ac.Engine)*0.5)
		ac.Status = StatusTakeoff
		ac.ClearedTakeoff = true
		ac.DepRunway = a.Runway
		ac.LandingRunway = a.Runway
		ac.InPattern = true
		ac.PatternTraffic = a.Traffic
		// Keep TG for next circuit.
		ac.LandingType = LandingTG
		ac.Instruction = "Touch and go runway " + a.Runway
		return changed

	case LandingSG:
		// Stop on runway, wait for timer or `go`.
		ac.Alt = field
		ac.Lat, ac.Lon = a.Threshold.Lat, a.Threshold.Lon
		ac.Heading = a.LandingHdg
		ac.Speed = 0
		ac.Status = StatusLanded
		ac.SGWaiting = true
		if ac.SGWaitSec > 0 {
			ac.SGTimer = ac.SGWaitSec
		} else {
			ac.SGTimer = 0 // wait forever for `go`
		}
		ac.DepRunway = a.Runway
		ac.LandingRunway = a.Runway
		ac.InPattern = true
		ac.PatternTraffic = a.Traffic
		ac.Instruction = "Stop and go, holding runway " + a.Runway
		return changed

	default: // LandingFS
		ac.Alt = field
		ac.Lat, ac.Lon = a.Threshold.Lat, a.Threshold.Lon
		ac.Heading = a.LandingHdg
		// Decelerate on the runway.
		if ac.Speed > 40 {
			ac.Speed = 40
		}
		ac.Status = StatusLanded
		ac.InPattern = false
		ac.SGWaiting = false
		ac.Instruction = "Landed runway " + a.Runway
		e.arr++
		return changed
	}
}

// tickSGWaitLocked counts down stop-and-go hold; auto-rolls when timer expires.
func (e *Engine) tickSGWaitLocked(ac *SimAircraft, dtSec float64) bool {
	if ac == nil || !ac.SGWaiting {
		return false
	}
	// Freeze on runway.
	if ac.Speed != 0 {
		ac.Speed = 0
	}
	if ac.SGWaitSec <= 0 {
		// Indefinite until `go` command.
		return false
	}
	ac.SGTimer -= dtSec
	if ac.SGTimer > 0 {
		return true
	}
	// Timer expired → roll.
	ac.SGWaiting = false
	ac.SGTimer = 0
	ac.Status = StatusTakeoff
	ac.ClearedTakeoff = true
	if ac.LandingRunway != "" {
		ac.DepRunway = ac.LandingRunway
	}
	if ac.PatternTraffic == "" {
		ac.PatternTraffic = "L"
	}
	ac.InPattern = true
	ac.LandingType = LandingTG
	e.alignTakeoffHeadingLocked(ac)
	ac.Instruction = "Stop and go, rolling"
	return true
}

// climbTowardRate is climbToward with a custom fpm rate.
func climbTowardRate(ac *SimAircraft, target, rateFpm, dtSec float64) bool {
	diff := target - ac.Alt
	if math.Abs(diff) <= altEqualEpsFt {
		if ac.Alt != target {
			ac.Alt = target
			return true
		}
		return false
	}
	if rateFpm <= 0 {
		rateFpm = climbRateFpm
	}
	step := rateFpm * dtSec / 60.0
	if math.Abs(diff) <= step {
		ac.Alt = target
		return true
	}
	if diff > 0 {
		ac.Alt += step
	} else {
		ac.Alt -= step
	}
	return true
}

// expandTickLocked runs kinematics for all aircraft. Caller holds e.mu.
// elapsed must already have been advanced by the caller when unpaused.
func (e *Engine) expandTickLocked(dtSec float64) TickResult {
	if len(e.aircraft) == 0 || dtSec <= 0 {
		return TickResult{}
	}
	keys := make([]string, 0, len(e.aircraft))
	for cs := range e.aircraft {
		keys = append(keys, cs)
	}
	sort.Strings(keys)

	var updates []AircraftSnapshot
	var deletes []string
	for _, cs := range keys {
		ac := e.aircraft[cs]
		changed, del := e.tickAircraftLocked(ac, dtSec)
		if del {
			deletes = append(deletes, cs)
			continue
		}
		if changed {
			updates = append(updates, ac.snapshot())
		}
	}
	for _, cs := range deletes {
		delete(e.aircraft, cs)
	}
	return TickResult{Updates: updates, Deletes: deletes}
}
