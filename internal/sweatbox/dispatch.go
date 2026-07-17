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
	case "taxi":
		return e.cmdTaxiLocked(target, args)
	case "hold":
		return e.cmdHoldLocked(target)
	case "res":
		return e.cmdResLocked(target)
	case "cross":
		return e.cmdCrossLocked(target, args)
	case "cto":
		return e.cmdCTOLocked(target, args, "")
	case "ctoc":
		return e.cmdCTOCLocked(target)
	case "ctomlt":
		return e.cmdCTOLocked(target, args, "L")
	case "ctomrt":
		return e.cmdCTOLocked(target, args, "R")
	case "nostop":
		return e.cmdNoStopLocked(target)
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
	case "fh":
		return e.cmdFlyHeadingLocked(target, args, TurnShortest, false)
	case "fhn":
		return e.cmdFlyHeadingLocked(target, args, TurnShortest, true)
	case "tr":
		return e.cmdFlyHeadingLocked(target, args, TurnRight, false)
	case "tl":
		return e.cmdFlyHeadingLocked(target, args, TurnLeft, false)
	case "fph":
		return e.cmdFlyPresentHeadingLocked(target)
	case "cm":
		return e.cmdClimbMaintainLocked(target, args)
	case "spd":
		return e.cmdSpeedLocked(target, args)
	case "fp":
		return e.cmdFlightPlanLocked(target, args, RulesIFR)
	case "vp":
		return e.cmdFlightPlanLocked(target, args, RulesVFR)
	case "remarks":
		return e.cmdRemarksLocked(target, args)
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
	if !ac.groundOK() {
		return CommandResult{OK: false, Message: "Not taxiing, parked, holding short, holding in position, or landed."}
	}
	// Already lined up: idempotent success (refresh instruction).
	if ac.Status == StatusHoldingInPosition && ac.PositionHold {
		ac.Instruction = "Position and hold"
		return CommandResult{OK: true}
	}
	ac.Status = StatusHoldingInPosition
	ac.PositionHold = true
	// LUAW cancels a prior takeoff clearance; instructor must re-issue cto.
	ac.ClearedTakeoff = false
	ac.PatternTraffic = ""
	// Line-up clears any active hold-short wait (runway entry).
	ac.HoldShortOf = ""
	if ac.DepRunway != "" {
		ac.Instruction = "Position and hold runway " + ac.DepRunway
	} else {
		ac.Instruction = "Position and hold"
	}
	return CommandResult{OK: true}
}

// --- Ground movement (PR 3b) ------------------------------------------------

// parseTaxiArgs splits "steps… [hs holds…]" into route steps and hold-shorts.
func parseTaxiArgs(args []string) (steps, holds []string) {
	hsIdx := -1
	for i, a := range args {
		if strings.EqualFold(a, "hs") {
			hsIdx = i
			break
		}
	}
	if hsIdx < 0 {
		return args, nil
	}
	steps = args[:hsIdx]
	if hsIdx+1 < len(args) {
		holds = args[hsIdx+1:]
	}
	return steps, holds
}

func (e *Engine) cmdTaxiLocked(target string, args []string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if e.graph == nil {
		return CommandResult{OK: false, Message: "No airport loaded."}
	}
	if !ac.groundOK() {
		return CommandResult{OK: false, Message: "Not taxiing, parked or holding short."}
	}
	steps, holds := parseTaxiArgs(args)
	if len(steps) == 0 {
		return CommandResult{OK: false, Message: "Must specify at least one taxiway, runway or parking space to taxi to."}
	}
	plan, planErr := e.graph.PlanTaxi(ac.surfaceForTaxi(), steps, holds)
	if planErr != "" {
		return CommandResult{OK: false, Message: planErr}
	}

	// Apply plan → aircraft path state for Tick (PR 4).
	ac.TaxiWaypoints = append([]Point(nil), plan.Waypoints...)
	ac.TaxiWPIndex = 0
	ac.TaxiHolds = append([]TaxiHold(nil), plan.HoldAt...)
	ac.TaxiSteps = append([]string(nil), plan.Steps...)
	ac.TaxiParking = plan.Parking
	ac.HoldShortOf = ""
	ac.PositionHold = false
	// New taxi cancels takeoff clearance (re-issue cto after re-route).
	ac.ClearedTakeoff = false
	ac.HasDepHeading = false
	ac.DepHeading = 0
	ac.PatternTraffic = ""

	// DepRunway from the instructor's last route token (preserves RwyB ends).
	// PlanTaxi canonicalizes runway steps to "A/B", so plan.Steps loses the end.
	// Non-runway / parking destinations clear any stale DepRunway.
	ac.DepRunway = e.depRunwayFromTaxiArgsLocked(steps, plan.Parking)

	ac.Status = StatusTaxiing
	ac.Instruction = formatTaxiInstruction(plan)
	// Leaving a parking spot: clear parked name once taxi starts (origin surface kept).
	if ac.Parking != "" && plan.Parking == "" {
		// Keep Parking empty while taxiing to runway; CurrentSurface still origin until Tick.
		ac.Parking = ""
	}
	return CommandResult{OK: true}
}

func formatTaxiInstruction(plan TaxiPlan) string {
	var b strings.Builder
	b.WriteString("Taxi")
	for _, s := range plan.Steps {
		b.WriteByte(' ')
		b.WriteString(s)
	}
	if plan.Parking != "" {
		b.WriteString(" @")
		b.WriteString(plan.Parking)
	}
	if len(plan.Holds) > 0 {
		b.WriteString(" hs")
		for _, h := range plan.Holds {
			b.WriteByte(' ')
			b.WriteString(h)
		}
	}
	return b.String()
}

// depRunwayFromTaxiArgsLocked returns the departure runway end designator for a
// successful taxi plan, derived from the instructor's raw step tokens (not the
// PlanTaxi-canonicalized Steps). Empty when the destination is parking, a
// taxiway-only route, or not a runway — callers assign this directly so stale
// DepRunway values are cleared on re-taxi.
func (e *Engine) depRunwayFromTaxiArgsLocked(rawSteps []string, parking string) string {
	if parking != "" {
		return ""
	}
	// Last non-empty raw step; skip trailing parking tokens (@name or bare parking).
	for i := len(rawSteps) - 1; i >= 0; i-- {
		tok := strings.TrimSpace(rawSteps[i])
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, "@") {
			return ""
		}
		if e.graph != nil {
			if s := e.graph.Surface(tok); s != nil && s.Kind == SurfaceParking {
				return ""
			}
		}
		return e.runwayEndLabelLocked(tok)
	}
	return ""
}

// runwayEndLabelLocked returns a preferred end designator for a runway surface
// name (e.g. "15" → "15", "33/15" → "33"). Empty if not a runway.
func (e *Engine) runwayEndLabelLocked(name string) string {
	if e.graph == nil {
		return ""
	}
	s := e.graph.Surface(name)
	if s == nil || s.Kind != SurfaceRunway {
		return ""
	}
	up := strings.ToUpper(strings.TrimSpace(name))
	if up == s.RwyA || up == s.RwyB {
		return up
	}
	// Combined name → RwyA as default dep end label.
	if s.RwyA != "" {
		return s.RwyA
	}
	return s.Name
}

func (e *Engine) sameSurfaceNameLocked(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	if e.graph == nil {
		return false
	}
	return sameSurface(e.graph, a, b)
}

func (e *Engine) cmdHoldLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if ac.Status != StatusTaxiing {
		return CommandResult{OK: false, Message: "Not taxiing."}
	}
	ac.Status = StatusHolding
	ac.Instruction = "Hold position"
	return CommandResult{OK: true}
}

func (e *Engine) cmdResLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	switch ac.Status {
	case StatusHolding:
		// Resume after present-position hold.
		if !ac.hasTaxiPath() {
			return CommandResult{OK: false, Message: "Not taxiing or holding short."}
		}
		ac.Status = StatusTaxiing
		ac.Instruction = formatTaxiInstruction(TaxiPlan{
			Steps:   ac.TaxiSteps,
			Holds:   holdNames(ac.TaxiHolds),
			Parking: ac.TaxiParking,
		})
		return CommandResult{OK: true}

	case StatusHoldingShort:
		// Cannot resume into the departure runway without pos/cto.
		if ac.DepRunway != "" && ac.HoldShortOf != "" &&
			e.sameSurfaceNameLocked(ac.HoldShortOf, ac.DepRunway) &&
			!ac.ClearedTakeoff && !ac.PositionHold {
			return CommandResult{OK: false, Message: "Can't resume taxi while holding short of departure runway, use the \"pos\" command."}
		}
		// Cross/clear current hold-short and continue.
		ac.removeHoldNamed(ac.HoldShortOf)
		ac.HoldShortOf = ""
		if ac.hasTaxiPath() {
			ac.Status = StatusTaxiing
			ac.Instruction = formatTaxiInstruction(TaxiPlan{
				Steps:   ac.TaxiSteps,
				Holds:   holdNames(ac.TaxiHolds),
				Parking: ac.TaxiParking,
			})
		} else {
			ac.Status = StatusTaxiing
			ac.Instruction = "Taxi"
		}
		return CommandResult{OK: true}

	case StatusTaxiing:
		// Already moving — silent success.
		return CommandResult{OK: true}

	default:
		return CommandResult{OK: false, Message: "Not taxiing or holding short."}
	}
}

func holdNames(holds []TaxiHold) []string {
	if len(holds) == 0 {
		return nil
	}
	out := make([]string, len(holds))
	for i, h := range holds {
		out[i] = h.Name
	}
	return out
}

// removeHoldNamed drops hold-shorts whose Name equals name (case-insensitive).
func (a *SimAircraft) removeHoldNamed(name string) {
	if a == nil || name == "" || len(a.TaxiHolds) == 0 {
		return
	}
	out := make([]TaxiHold, 0, len(a.TaxiHolds))
	for _, h := range a.TaxiHolds {
		if strings.EqualFold(h.Name, name) {
			continue
		}
		out = append(out, h)
	}
	a.TaxiHolds = out
}

func (e *Engine) cmdCrossLocked(target string, args []string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if len(args) < 1 {
		return CommandResult{OK: false, Message: "Missing parameters. Example: \"cross 27\""}
	}
	name := strings.ToUpper(strings.TrimSpace(args[0]))
	if name == "" {
		return CommandResult{OK: false, Message: "Missing parameters. Example: \"cross 27\""}
	}
	// Must be holding short of it, or planning to (in TaxiHolds).
	holding := ac.Status == StatusHoldingShort && ac.HoldShortOf != "" &&
		e.sameSurfaceNameLocked(ac.HoldShortOf, name)
	planning := e.holdPlannedLocked(ac, name)
	if !holding && !planning {
		return CommandResult{OK: false, Message: "Not holding short of that runway/taxiway, and not planning to."}
	}
	// Remove from plan and clear active hold.
	ac.removeHoldNamed(name)
	// Also remove graph-canonical aliases (e.g. "19" vs "19/1").
	if e.graph != nil {
		if s := e.graph.Surface(name); s != nil {
			ac.removeHoldNamed(s.Name)
			if s.Kind == SurfaceRunway {
				ac.removeHoldNamed(s.RwyA)
				ac.removeHoldNamed(s.RwyB)
			}
		}
	}
	if ac.HoldShortOf != "" && e.sameSurfaceNameLocked(ac.HoldShortOf, name) {
		ac.HoldShortOf = ""
	}
	// Resume taxi if we were stopped for hold-short or present-position hold.
	switch ac.Status {
	case StatusHoldingShort, StatusHolding:
		ac.Status = StatusTaxiing
	}
	ac.Instruction = "Cross " + name
	return CommandResult{OK: true}
}

func (e *Engine) holdPlannedLocked(ac *SimAircraft, name string) bool {
	if ac == nil || name == "" {
		return false
	}
	for _, h := range ac.TaxiHolds {
		if e.sameSurfaceNameLocked(h.Name, name) {
			return true
		}
	}
	return false
}

// cmdCTOLocked implements cto / ctomlt / ctomrt.
// patternDir is "", "L", or "R".
func (e *Engine) cmdCTOLocked(target string, args []string, patternDir string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	switch ac.Status {
	case StatusTaxiing, StatusHoldingShort, StatusHoldingInPosition, StatusHolding:
		// ok — "holding short, in position, or any time during taxi to the runway"
	case StatusTakeoff:
		// Re-issue / update heading while rolling.
	default:
		return CommandResult{OK: false, Message: "Not taxiing, holding short or in position."}
	}

	// Optional departure heading (plain cto only; ctomlt/ctomrt take no args).
	if patternDir == "" && len(args) >= 1 {
		hdg, ok := parseFiniteFloat(args[0])
		if !ok {
			return CommandResult{OK: false, Message: "Missing parameters. Example: \"cto 140\""}
		}
		ac.DepHeading = normalizeHeading(hdg)
		ac.HasDepHeading = true
	}

	ac.ClearedTakeoff = true
	ac.PositionHold = false
	if patternDir != "" {
		ac.PatternTraffic = patternDir
	} else {
		// Plain cto clears closed-traffic intent from a prior ctomlt/ctomrt.
		ac.PatternTraffic = ""
	}

	// If already at the runway (in position or holding short of dep), enter takeoff state.
	atRunway := ac.Status == StatusHoldingInPosition ||
		(ac.Status == StatusHoldingShort && ac.DepRunway != "" &&
			ac.HoldShortOf != "" && e.sameSurfaceNameLocked(ac.HoldShortOf, ac.DepRunway))
	if atRunway {
		ac.Status = StatusTakeoff
		ac.HoldShortOf = ""
		// Align ground-roll heading to runway (taxi entry is often perpendicular).
		e.alignTakeoffHeadingLocked(ac)
	}

	ac.Instruction = formatCTOInstruction(ac)
	return CommandResult{OK: true}
}

func formatCTOInstruction(ac *SimAircraft) string {
	var b strings.Builder
	b.WriteString("Cleared for takeoff")
	if ac.DepRunway != "" {
		b.WriteString(" runway ")
		b.WriteString(ac.DepRunway)
	}
	if ac.HasDepHeading {
		b.WriteString(fmt.Sprintf(" heading %03.0f", ac.DepHeading))
	}
	switch ac.PatternTraffic {
	case "L":
		b.WriteString(", left traffic")
	case "R":
		b.WriteString(", right traffic")
	}
	return b.String()
}

func (e *Engine) cmdCTOCLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if !ac.ClearedTakeoff {
		return CommandResult{OK: false, Message: "Not cleared for takeoff."}
	}
	ac.ClearedTakeoff = false
	ac.HasDepHeading = false
	ac.DepHeading = 0
	ac.PatternTraffic = ""
	// If takeoff roll not yet airborne, return to hold-short / position.
	switch ac.Status {
	case StatusTakeoff:
		if ac.DepRunway != "" {
			// Prefer holding short of the departure runway (safer cancel).
			ac.Status = StatusHoldingShort
			ac.HoldShortOf = ac.DepRunway
			ac.PositionHold = false
			ac.Instruction = "Holding short of " + ac.DepRunway
		} else if ac.hasTaxiPath() {
			ac.Status = StatusTaxiing
			ac.Instruction = formatTaxiInstruction(TaxiPlan{
				Steps:   ac.TaxiSteps,
				Holds:   holdNames(ac.TaxiHolds),
				Parking: ac.TaxiParking,
			})
		} else {
			ac.Status = StatusHoldingInPosition
			ac.PositionHold = true
			ac.Instruction = "Position and hold"
		}
	default:
		ac.Instruction = "Takeoff clearance cancelled"
	}
	return CommandResult{OK: true}
}

func (e *Engine) cmdNoStopLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	// nostop applies mainly to arrivals on the roll / after landing, but
	// allowing it on any aircraft is harmless (Tick honors when relevant).
	ac.NoStop = true
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

// cmdFlyHeadingLocked implements fh / fhn / tr / tl.
// immediate (fhn) snaps Heading now and sets ImmediateHeading for the tick.
func (e *Engine) cmdFlyHeadingLocked(target string, args []string, turnDir int, immediate bool) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if len(args) < 1 {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "fh 120"`}
	}
	hdg, ok := parseFiniteFloat(args[0])
	if !ok {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "fh 120"`}
	}
	hdg = normalizeHeading(hdg)
	ac.DesiredHeading = hdg
	ac.HasDesiredHeading = true
	ac.TurnDir = turnDir
	ac.ImmediateHeading = immediate
	if immediate {
		// fhn: snap domain heading immediately (no realistic turn).
		ac.Heading = hdg
	}
	switch turnDir {
	case TurnRight:
		ac.Instruction = fmt.Sprintf("Turn right heading %03.0f", hdg)
	case TurnLeft:
		ac.Instruction = fmt.Sprintf("Turn left heading %03.0f", hdg)
	default:
		ac.Instruction = fmt.Sprintf("Fly heading %03.0f", hdg)
	}
	return CommandResult{OK: true}
}

func (e *Engine) cmdFlyPresentHeadingLocked(target string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	hdg := normalizeHeading(ac.Heading)
	ac.DesiredHeading = hdg
	ac.HasDesiredHeading = true
	ac.TurnDir = TurnShortest
	ac.ImmediateHeading = false
	ac.Instruction = "Fly present heading"
	return CommandResult{OK: true}
}

func (e *Engine) cmdClimbMaintainLocked(target string, args []string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if len(args) < 1 {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "cm 14000"`}
	}
	alt, ok := parseFiniteFloat(args[0])
	if !ok {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "cm 14000"`}
	}
	ac.DesiredAlt = alt
	ac.HasDesiredAlt = true
	switch {
	case alt > ac.Alt+0.5:
		ac.Instruction = fmt.Sprintf("Climb and maintain %.0f", alt)
	case alt < ac.Alt-0.5:
		ac.Instruction = fmt.Sprintf("Descend and maintain %.0f", alt)
	default:
		ac.Instruction = fmt.Sprintf("Maintain %.0f", alt)
	}
	return CommandResult{OK: true}
}

func (e *Engine) cmdSpeedLocked(target string, args []string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if len(args) < 1 {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "spd 250"`}
	}
	spd, ok := parseFiniteFloat(args[0])
	if !ok || spd < 0 {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "spd 250"`}
	}
	ac.DesiredSpeed = spd
	ac.HasDesiredSpeed = true
	ac.Instruction = fmt.Sprintf("Speed %.0f", spd)
	return CommandResult{OK: true}
}

// cmdFlightPlanLocked implements fp (IFR) and vp (VFR).
// Syntax: fp|vp type altitude route...
// Altitude: values in [1, 999] are treated as flight levels (×100 feet);
// otherwise feet MSL (matches TWRTrainer examples: "fp b738 220 …" vs "vp c172 8500 …").
func (e *Engine) cmdFlightPlanLocked(target string, args []string, rules string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	usage := `Missing parameters. Example: "fp b738 220 kbos dct kjfk"`
	if rules == RulesVFR {
		usage = `Missing parameters. Example: "vp c172 8500 kbos dct kbtv"`
	}
	if len(args) < 2 {
		return CommandResult{OK: false, Message: usage}
	}
	acType := strings.ToUpper(strings.TrimSpace(args[0]))
	if acType == "" {
		return CommandResult{OK: false, Message: usage}
	}
	altRaw, ok := parseFiniteFloat(args[1])
	if !ok || altRaw < 0 {
		return CommandResult{OK: false, Message: usage}
	}
	cruise := parseCruiseAltitude(altRaw)

	routeToks := args[2:]
	route := strings.Join(routeToks, " ")

	ac.Rules = rules
	ac.Type = acType
	ac.CruiseAlt = cruise
	ac.Route = route
	// When the route starts/ends with airport-like tokens, update Dep/Arr.
	if len(routeToks) >= 1 && looksLikeAirport(routeToks[0]) {
		ac.Dep = strings.ToUpper(routeToks[0])
	}
	if len(routeToks) >= 2 && looksLikeAirport(routeToks[len(routeToks)-1]) {
		ac.Arr = strings.ToUpper(routeToks[len(routeToks)-1])
	}
	return CommandResult{OK: true}
}

func (e *Engine) cmdRemarksLocked(target string, args []string) CommandResult {
	ac, errMsg := e.requireAircraftLocked(target)
	if errMsg != "" {
		return CommandResult{OK: false, Message: errMsg}
	}
	if len(args) < 1 {
		return CommandResult{OK: false, Message: `Missing parameters. Example: "remarks Request VFR closed traffic"`}
	}
	// Preserve instructor casing/spacing of remarks text (tokens re-joined).
	ac.Remarks = strings.Join(args, " ")
	return CommandResult{OK: true}
}

// parseCruiseAltitude converts a raw altitude number to feet MSL.
// Values in (0, 1000) are treated as flight levels (hundreds of feet).
func parseCruiseAltitude(raw float64) int {
	if raw > 0 && raw < 1000 {
		return int(math.Round(raw * 100))
	}
	return int(math.Round(raw))
}

// looksLikeAirport reports whether s looks like an ICAO/IATA airport code
// (3–4 alphabetic characters). Used only as a Dep/Arr hint for fp/vp.
func looksLikeAirport(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
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
