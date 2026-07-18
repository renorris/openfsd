package sweatbox

import (
	"fmt"
	"math"
	"strings"

	"github.com/renorris/openfsd/internal/geo"
)

// Pattern geometry and landing-type helpers (P1).
//
// A rectangular left/right traffic pattern is built from the runway threshold,
// far end, landing heading, traffic side, and pattern size (NM). Pattern size
// sets both the downwind offset (crosswind/base length) and the final length
// (TWRTrainer: "how far the downwind is from the runway, and how long the
// final leg will be").

const (
	// Pattern size clamp (TWRTrainer ps command).
	minPatternSizeNM = 0.5
	maxPatternSizeNM = 20.0

	// Low-approach AGL (feet).
	lowApproachAGLFt = 200.0

	// Corner / threshold arrival tolerances.
	patternCornerEpsM = 185.0 // ~0.1 NM
	patternThreshEpsM = 80.0  // ~260 ft — near threshold for landing/overfly
	midfieldEpsM      = 400.0 // midfield report window

	// Minimum upwind extension past the far end (NM).
	minUpwindExtNM = 0.25
)

// PatternAnchors holds the rectangular circuit geometry for one runway end +
// traffic direction + size.
type PatternAnchors struct {
	Threshold  Point
	FarEnd     Point
	LandingHdg float64
	LatHdg     float64 // lateral heading toward the traffic side (L = H-90, R = H+90)
	Traffic    string  // "L" or "R"
	SizeNM     float64
	Runway     string // resolved end designator

	// Turn points (fly toward these to complete the named leg).
	UpwindEnd    Point // end of upwind → turn crosswind
	CrosswindEnd Point // end of crosswind → turn downwind
	DownwindEnd  Point // end of downwind → turn base
	BaseEnd      Point // end of base → turn final (= FinalStart)
	FinalStart   Point // start of final (also BaseEnd)
	MidfieldDW   Point // midfield downwind (abeam runway mid)
}

// computePatternAnchors builds pattern geometry for a runway end.
// traffic is "L" or "R"; sizeNM is the pattern size in NM (already clamped).
// errMsg is non-empty on failure (soft instructor message).
func computePatternAnchors(s *Surface, rwyEnd, traffic string, sizeNM float64) (PatternAnchors, string) {
	var a PatternAnchors
	if s == nil || s.Kind != SurfaceRunway {
		return a, "Runway/taxiway not found in airport file."
	}
	thr, hdg, ok := runwayThreshold(s, rwyEnd)
	if !ok {
		return a, "Runway/taxiway not found in airport file."
	}
	// Far end is the opposite threshold without displacement for geometry.
	var far Point
	end := strings.ToUpper(strings.TrimSpace(rwyEnd))
	if end == s.Name || end == s.RwyA+"/"+s.RwyB {
		end = s.RwyA
	}
	switch end {
	case s.RwyA:
		far = s.Points[len(s.Points)-1]
	case s.RwyB:
		far = s.Points[0]
	default:
		return a, "Runway/taxiway not found in airport file."
	}

	traffic = strings.ToUpper(strings.TrimSpace(traffic))
	if traffic != "L" && traffic != "R" {
		traffic = "L"
	}
	if sizeNM < minPatternSizeNM {
		sizeNM = minPatternSizeNM
	}
	if sizeNM > maxPatternSizeNM {
		sizeNM = maxPatternSizeNM
	}

	latHdg := normalizeHeading(hdg - 90)
	if traffic == "R" {
		latHdg = normalizeHeading(hdg + 90)
	}
	sizeM := sizeNM * metersPerNM
	upwindExtNM := sizeNM * 0.25
	if upwindExtNM < minUpwindExtNM {
		upwindExtNM = minUpwindExtNM
	}
	upwindExtM := upwindExtNM * metersPerNM

	upwindEndLat, upwindEndLon := destinationPoint(far.Lat, far.Lon, hdg, upwindExtM)
	cwEndLat, cwEndLon := destinationPoint(upwindEndLat, upwindEndLon, latHdg, sizeM)
	finalStartLat, finalStartLon := destinationPoint(thr.Lat, thr.Lon, normalizeHeading(hdg+180), sizeM)
	dwEndLat, dwEndLon := destinationPoint(finalStartLat, finalStartLon, latHdg, sizeM)
	mid := Point{
		Lat: (thr.Lat + far.Lat) / 2,
		Lon: (thr.Lon + far.Lon) / 2,
	}
	mfLat, mfLon := destinationPoint(mid.Lat, mid.Lon, latHdg, sizeM)

	a = PatternAnchors{
		Threshold:    thr,
		FarEnd:       far,
		LandingHdg:   hdg,
		LatHdg:       latHdg,
		Traffic:      traffic,
		SizeNM:       sizeNM,
		Runway:       end,
		UpwindEnd:    Point{Lat: upwindEndLat, Lon: upwindEndLon},
		CrosswindEnd: Point{Lat: cwEndLat, Lon: cwEndLon},
		DownwindEnd:  Point{Lat: dwEndLat, Lon: dwEndLon},
		BaseEnd:      Point{Lat: finalStartLat, Lon: finalStartLon},
		FinalStart:   Point{Lat: finalStartLat, Lon: finalStartLon},
		MidfieldDW:   Point{Lat: mfLat, Lon: mfLon},
	}
	return a, ""
}

// legTarget returns the point the aircraft flies toward while on leg.
func (a PatternAnchors) legTarget(leg string) Point {
	switch leg {
	case StatusUpwind:
		return a.UpwindEnd
	case StatusCrosswind:
		return a.CrosswindEnd
	case StatusDownwind:
		return a.DownwindEnd
	case StatusBase:
		return a.BaseEnd
	case StatusFinal:
		return a.Threshold
	default:
		return a.Threshold
	}
}

// legHeading is the nominal track for a leg.
func (a PatternAnchors) legHeading(leg string) float64 {
	switch leg {
	case StatusUpwind:
		return a.LandingHdg
	case StatusCrosswind:
		return a.LatHdg
	case StatusDownwind:
		return normalizeHeading(a.LandingHdg + 180)
	case StatusBase:
		// From downwind end toward final start: opposite of lateral.
		return normalizeHeading(a.LatHdg + 180)
	case StatusFinal:
		return a.LandingHdg
	default:
		return a.LandingHdg
	}
}

// nextLeg returns the following pattern leg (left/right order is the same:
// Upwind→Crosswind→Downwind→Base→Final→Upwind).
func nextLeg(leg string) string {
	switch leg {
	case StatusUpwind:
		return StatusCrosswind
	case StatusCrosswind:
		return StatusDownwind
	case StatusDownwind:
		return StatusBase
	case StatusBase:
		return StatusFinal
	case StatusFinal:
		return StatusUpwind
	default:
		return StatusUpwind
	}
}

// prevLeg is the reverse of nextLeg.
func prevLeg(leg string) string {
	switch leg {
	case StatusCrosswind:
		return StatusUpwind
	case StatusDownwind:
		return StatusCrosswind
	case StatusBase:
		return StatusDownwind
	case StatusFinal:
		return StatusBase
	case StatusUpwind:
		return StatusFinal
	default:
		return StatusUpwind
	}
}

// turnCommandForLeg maps tc/td/tb to the leg they turn onto.
func turnCommandTargetLeg(verb string) string {
	switch strings.ToLower(verb) {
	case "tc", "tcn":
		return StatusCrosswind
	case "td", "tdn":
		return StatusDownwind
	case "tb", "tbn":
		return StatusBase
	default:
		return ""
	}
}

// effectivePatternSizeNM returns the aircraft override or airport default.
func (e *Engine) effectivePatternSizeNMLocked(ac *SimAircraft) float64 {
	if ac != nil && ac.PatternSizeNM >= minPatternSizeNM {
		s := ac.PatternSizeNM
		if s > maxPatternSizeNM {
			s = maxPatternSizeNM
		}
		return s
	}
	if e.airport != nil && e.airport.PatternSize >= minPatternSizeNM {
		s := e.airport.PatternSize
		if s > maxPatternSizeNM {
			s = maxPatternSizeNM
		}
		return s
	}
	return 1.0
}

// patternAltitudeMSL returns the MSL altitude for pattern legs.
func patternAltitudeMSL(apt *Airport) float64 {
	if apt == nil {
		return 1000
	}
	if apt.PatternElev > 0 {
		return apt.PatternElev
	}
	return apt.FieldElev + 1000
}

// resolveRunwaySurface finds a runway surface + end label for pattern commands.
func (e *Engine) resolveRunwaySurfaceLocked(rwy string) (*Surface, string, string) {
	rwy = strings.ToUpper(strings.TrimSpace(rwy))
	if rwy == "" || e.airport == nil {
		return nil, "", "Runway/taxiway not found in airport file."
	}
	var s *Surface
	if e.graph != nil {
		s = e.graph.Surface(rwy)
	}
	if s == nil {
		s = e.airport.FindSurface(rwy)
	}
	if s == nil || s.Kind != SurfaceRunway {
		return nil, "", "Runway/taxiway not found in airport file."
	}
	end := rwy
	if rwy == s.Name || rwy == s.RwyA+"/"+s.RwyB {
		end = s.RwyA
	}
	if end != s.RwyA && end != s.RwyB {
		// Combined name already handled; unknown end token.
		if end == s.Name {
			end = s.RwyA
		}
	}
	return s, end, ""
}

// anchorsForAircraft builds anchors from the aircraft's landing runway + traffic + size.
func (e *Engine) anchorsForAircraftLocked(ac *SimAircraft) (PatternAnchors, string) {
	if ac == nil {
		return PatternAnchors{}, "No aircraft selected."
	}
	rwy := ac.LandingRunway
	if rwy == "" {
		rwy = ac.DepRunway
	}
	if rwy == "" {
		return PatternAnchors{}, "No runway assigned for pattern."
	}
	s, end, errMsg := e.resolveRunwaySurfaceLocked(rwy)
	if errMsg != "" {
		return PatternAnchors{}, errMsg
	}
	traffic := ac.PatternTraffic
	if traffic != "L" && traffic != "R" {
		traffic = "L"
	}
	return computePatternAnchors(s, end, traffic, e.effectivePatternSizeNMLocked(ac))
}

// placeOnPatternLegLocked snaps ac onto a pattern leg at a sensible entry point.
func placeOnPatternLegLocked(ac *SimAircraft, a PatternAnchors, leg string, apt *Airport) {
	if ac == nil {
		return
	}
	patAlt := patternAltitudeMSL(apt)
	field := fieldElevFeet(apt)

	switch leg {
	case StatusUpwind:
		// Just past threshold along landing heading.
		lat, lon := destinationPoint(a.Threshold.Lat, a.Threshold.Lon, a.LandingHdg, 200)
		ac.Lat, ac.Lon = lat, lon
		ac.Heading = a.LandingHdg
		ac.Alt = patAlt
	case StatusCrosswind:
		ac.Lat, ac.Lon = a.UpwindEnd.Lat, a.UpwindEnd.Lon
		ac.Heading = a.LatHdg
		ac.Alt = patAlt
	case StatusDownwind:
		ac.Lat, ac.Lon = a.MidfieldDW.Lat, a.MidfieldDW.Lon
		ac.Heading = a.legHeading(StatusDownwind)
		ac.Alt = patAlt
	case StatusBase:
		ac.Lat, ac.Lon = a.DownwindEnd.Lat, a.DownwindEnd.Lon
		ac.Heading = a.legHeading(StatusBase)
		ac.Alt = patAlt
	case StatusFinal:
		ac.Lat, ac.Lon = a.FinalStart.Lat, a.FinalStart.Lon
		ac.Heading = a.LandingHdg
		// Glideslope altitude for pattern-size final.
		ac.Alt = approachAltitude(field, a.SizeNM)
	default:
		ac.Lat, ac.Lon = a.MidfieldDW.Lat, a.MidfieldDW.Lon
		ac.Heading = a.legHeading(StatusDownwind)
		ac.Alt = patAlt
		leg = StatusDownwind
	}

	ac.Status = leg
	ac.InPattern = true
	ac.LandingRunway = a.Runway
	ac.PatternTraffic = a.Traffic
	ac.ExtendLeg = false
	ac.MidfieldReported = leg != StatusDownwind // re-report only if entering downwind midfield
	if leg == StatusDownwind {
		ac.MidfieldReported = false
	}
	ac.SGWaiting = false
	ac.SGTimer = 0
	// Clear conflicting ground / vector state.
	ac.TaxiWaypoints = nil
	ac.TaxiWPIndex = 0
	ac.TaxiHolds = nil
	ac.TaxiSteps = nil
	ac.TaxiParking = ""
	ac.HoldShortOf = ""
	ac.PositionHold = false
	ac.ClearedTakeoff = false
	ac.Parking = ""
	ac.CurrentSurface = ""
	// Seed air targets for the leg.
	ac.DesiredHeading = a.legHeading(leg)
	ac.HasDesiredHeading = true
	ac.TurnDir = patternTurnDir(a.Traffic)
	ac.ImmediateHeading = true
	ac.Heading = ac.DesiredHeading
	ac.DesiredAlt = ac.Alt
	ac.HasDesiredAlt = true
	if !ac.HasDesiredSpeed || ac.DesiredSpeed <= 0 {
		ac.DesiredSpeed = defaultApproachSpeed(ac.Engine)
		ac.HasDesiredSpeed = true
	}
	if ac.Speed < 40 {
		ac.Speed = ac.DesiredSpeed
	}
	// Default closed-traffic landing type.
	if ac.LandingType == "" {
		ac.LandingType = LandingTG
	}
	ac.Instruction = formatPatternInstruction(ac)
}

// patternTurnDir returns TurnLeft for left traffic, TurnRight for right.
func patternTurnDir(traffic string) int {
	if traffic == "R" {
		return TurnRight
	}
	return TurnLeft
}

// formatPatternInstruction builds a status-line instruction for pattern work.
func formatPatternInstruction(ac *SimAircraft) string {
	if ac == nil {
		return ""
	}
	leg := ac.Status
	rwy := ac.LandingRunway
	if rwy == "" {
		rwy = ac.DepRunway
	}
	var b strings.Builder
	switch leg {
	case StatusUpwind, StatusCrosswind, StatusDownwind, StatusBase, StatusFinal:
		b.WriteString(leg)
	default:
		if ac.InPattern {
			b.WriteString("Pattern")
		} else {
			return ac.Instruction
		}
	}
	if rwy != "" {
		b.WriteString(" runway ")
		b.WriteString(rwy)
	}
	switch ac.PatternTraffic {
	case "L":
		b.WriteString(", left traffic")
	case "R":
		b.WriteString(", right traffic")
	}
	if ac.ExtendLeg {
		b.WriteString(" (extending)")
	}
	if ac.ShortApproach && (leg == StatusDownwind || leg == StatusBase || leg == StatusFinal) {
		b.WriteString(", short approach")
	}
	switch ac.LandingType {
	case LandingTG:
		b.WriteString(", touch and go")
	case LandingSG:
		b.WriteString(", stop and go")
	case LandingLA:
		b.WriteString(", low approach")
	case LandingFS:
		b.WriteString(", full stop")
	}
	return b.String()
}

// advancePatternLegLocked moves to the next leg (or short-approach final).
func (e *Engine) advancePatternLegLocked(ac *SimAircraft, a PatternAnchors) {
	if ac == nil {
		return
	}
	cur := ac.Status
	var next string
	if ac.ShortApproach && cur == StatusDownwind {
		next = StatusFinal
	} else {
		next = nextLeg(cur)
	}
	ac.Status = next
	ac.ExtendLeg = false
	ac.DesiredHeading = a.legHeading(next)
	ac.HasDesiredHeading = true
	ac.TurnDir = patternTurnDir(a.Traffic)
	ac.ImmediateHeading = false
	if next == StatusFinal {
		// Start descending on final.
		field := fieldElevFeet(e.airport)
		distNM := geo.Distance(ac.Lat, ac.Lon, a.Threshold.Lat, a.Threshold.Lon) / metersPerNM
		ac.DesiredAlt = approachAltitude(field, distNM)
		ac.HasDesiredAlt = true
	} else {
		ac.DesiredAlt = patternAltitudeMSL(e.airport)
		ac.HasDesiredAlt = true
	}
	if next == StatusDownwind {
		ac.MidfieldReported = false
	}
	ac.Instruction = formatPatternInstruction(ac)
}

// enterPatternFromTakeoffLocked promotes a closed-traffic takeoff into Upwind.
func (e *Engine) enterPatternFromTakeoffLocked(ac *SimAircraft) {
	if ac == nil {
		return
	}
	rwy := ac.DepRunway
	if rwy == "" {
		rwy = ac.LandingRunway
	}
	if rwy == "" || ac.PatternTraffic == "" {
		return
	}
	s, end, errMsg := e.resolveRunwaySurfaceLocked(rwy)
	if errMsg != "" {
		return
	}
	a, errMsg := computePatternAnchors(s, end, ac.PatternTraffic, e.effectivePatternSizeNMLocked(ac))
	if errMsg != "" {
		return
	}
	ac.LandingRunway = a.Runway
	ac.InPattern = true
	ac.Status = StatusUpwind
	ac.ExtendLeg = false
	if ac.LandingType == "" {
		ac.LandingType = LandingTG
	}
	ac.DesiredHeading = a.legHeading(StatusUpwind)
	ac.HasDesiredHeading = true
	ac.TurnDir = patternTurnDir(a.Traffic)
	ac.ImmediateHeading = false
	ac.DesiredAlt = patternAltitudeMSL(e.airport)
	ac.HasDesiredAlt = true
	if !ac.HasDesiredSpeed {
		ac.DesiredSpeed = defaultApproachSpeed(ac.Engine)
		ac.HasDesiredSpeed = true
	}
	ac.ClearedTakeoff = false
	ac.Instruction = formatPatternInstruction(ac)
}

// distPoint returns great-circle distance meters between ac and p.
func distToPoint(ac *SimAircraft, p Point) float64 {
	if ac == nil {
		return math.MaxFloat64
	}
	return geo.Distance(ac.Lat, ac.Lon, p.Lat, p.Lon)
}

// formatLandingTypeInstruction is a short phrase for tg/sg/la/fs command acks.
func formatLandingTypeName(code string) string {
	switch code {
	case LandingTG:
		return "Touch and go"
	case LandingSG:
		return "Stop and go"
	case LandingLA:
		return "Low approach"
	case LandingFS:
		return "Full stop"
	default:
		return code
	}
}

// patternLegFromEnterVerb maps erc/erd/… to a status leg + traffic side.
func patternLegFromEnterVerb(verb string) (leg, traffic string, ok bool) {
	switch strings.ToLower(verb) {
	case "erc":
		return StatusCrosswind, "R", true
	case "erd":
		return StatusDownwind, "R", true
	case "erb":
		return StatusBase, "R", true
	case "elc":
		return StatusCrosswind, "L", true
	case "eld":
		return StatusDownwind, "L", true
	case "elb":
		return StatusBase, "L", true
	case "ef":
		return StatusFinal, "", true // traffic left as-is / default L
	default:
		return "", "", false
	}
}

// clampPatternSize clamps size to [0.5, 20].
func clampPatternSize(size float64) float64 {
	if size < minPatternSizeNM {
		return minPatternSizeNM
	}
	if size > maxPatternSizeNM {
		return maxPatternSizeNM
	}
	return size
}

// patternEntryUsage is the soft error for enter-* commands missing a runway.
func patternEntryUsage(verb string) string {
	return fmt.Sprintf(`Missing parameters. Example: "%s 27"`, verb)
}
