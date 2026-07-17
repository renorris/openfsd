package sweatbox

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

// Defaults matching design doc / TWRTrainer resource limits.
const (
	DefaultMaxAircraft = 64
	HardMaxAircraft    = 128
	metersPerNM        = 1852.0
	feetPerNM          = 6076.12
	// ~3° glideslope: tan(3°) * feetPerNM ≈ 318.4 ft per NM.
	glideslopeFtPerNM = 318.4
)

// Settings controls engine resource limits and future motion knobs.
type Settings struct {
	// MaxAircraft defaults to 64 and is clamped to [1, 128].
	MaxAircraft int
	// DeleteArrivalsWhenParked is reserved for taxi-to-parking (PR 3b+).
	DeleteArrivalsWhenParked bool
	// IntersectionTolM is stored for graph rebuilds; 0 → default 100 ft.
	IntersectionTolM float64
}

// normalize clamps MaxAircraft and fills defaults.
func (s Settings) normalize() Settings {
	if s.MaxAircraft <= 0 {
		s.MaxAircraft = DefaultMaxAircraft
	}
	if s.MaxAircraft > HardMaxAircraft {
		s.MaxAircraft = HardMaxAircraft
	}
	if s.IntersectionTolM <= 0 {
		s.IntersectionTolM = DefaultIntersectionTolM
	}
	return s
}

// Engine is the pure sweatbox simulator core: airport, aircraft map, pause,
// elapsed/ops counters, and instructor commands. No I/O; all mutations serialize
// on mu. Never hold mu across network or host work (callers copy results out).
type Engine struct {
	mu sync.Mutex

	settings Settings
	airport  *Airport
	graph    *Graph
	aircraft map[string]*SimAircraft // upper-case callsign → state

	paused  bool
	elapsed time.Duration
	arr     int
	dep     int

	// generators for unique callsigns / squawks
	csSeq  int
	sqkSeq int
}

// NewEngine constructs an empty paused engine with default settings.
func NewEngine() *Engine {
	return NewEngineSettings(Settings{})
}

// NewEngineSettings constructs an empty paused engine with the given settings.
func NewEngineSettings(s Settings) *Engine {
	return &Engine{
		settings: s.normalize(),
		aircraft: make(map[string]*SimAircraft),
		paused:   true,
		sqkSeq:   2000,
	}
}

// Settings returns a copy of the current settings.
func (e *Engine) Settings() Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.settings
}

// SetMaxAircraft updates the aircraft cap (clamped). Existing aircraft are kept
// even if above the new cap; further adds are rejected until under the cap.
func (e *Engine) SetMaxAircraft(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.settings.MaxAircraft = n
	e.settings = e.settings.normalize()
}

// LoadAirport installs airport geometry and rebuilds the taxi graph.
// Fails if aircraft are present (caller must Delete all or use replace flow).
func (e *Engine) LoadAirport(apt *Airport) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if apt == nil {
		return fmt.Errorf("airport is nil")
	}
	if len(e.aircraft) > 0 {
		return fmt.Errorf("cannot load airport while %d aircraft are present", len(e.aircraft))
	}
	// Copy header/surfaces so later external mutation does not affect the engine.
	cp := *apt
	cp.Surfaces = append([]Surface(nil), apt.Surfaces...)
	for i := range cp.Surfaces {
		cp.Surfaces[i].Points = append([]Point(nil), apt.Surfaces[i].Points...)
	}
	e.airport = &cp
	e.graph = NewGraphTol(&cp, e.settings.IntersectionTolM)
	return nil
}

// LoadScenario loads .air-style rows into the engine (best-effort).
// Always auto-pauses. Returns loaded callsigns and per-row error messages.
// Requires an airport. Duplicate callsigns and max-cap violations are skipped.
func (e *Engine) LoadScenario(rows []Aircraft) (loaded []string, errs []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = true
	if e.airport == nil {
		return nil, []string{"No airport loaded."}
	}
	for _, row := range rows {
		ac := fromScenarioRow(row)
		if ac.Callsign == "" {
			errs = append(errs, "Missing callsign")
			continue
		}
		if _, exists := e.aircraft[ac.Callsign]; exists {
			errs = append(errs, formatCallsignInUse(ac.Callsign))
			continue
		}
		if len(e.aircraft) >= e.settings.MaxAircraft {
			errs = append(errs, fmt.Sprintf("Maximum aircraft (%d) reached; skipped %s", e.settings.MaxAircraft, ac.Callsign))
			continue
		}
		if ac.Squawk == "" || !isSquawk(ac.Squawk) {
			ac.Squawk = e.nextSquawkLocked(ac.Rules)
		}
		if ac.XPDRMode == "" {
			ac.XPDRMode = XPDRModeNormal
		}
		e.aircraft[ac.Callsign] = ac
		loaded = append(loaded, ac.Callsign)
	}
	return loaded, errs
}

// Pause freezes motion and elapsed accumulation.
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = true
}

// Unpause resumes motion and elapsed accumulation.
func (e *Engine) Unpause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = false
}

// Paused reports whether the simulation is paused.
func (e *Engine) Paused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.paused
}

// Elapsed returns accumulated unpaused simulation time.
func (e *Engine) Elapsed() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.elapsed
}

// Tick advances simulation by dt when unpaused.
// PR 3a: only elapsed time advances; kinematics land in PR 4.
// Returns empty result copies for host apply (no shared state).
func (e *Engine) Tick(dt time.Duration) TickResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.paused || dt <= 0 {
		return TickResult{}
	}
	e.elapsed += dt
	return TickResult{}
}

// TickResult is the set of host-facing mutations from one tick (value copies).
type TickResult struct {
	Updates []AircraftSnapshot
	Deletes []string
}

// Delete removes an aircraft by callsign. Idempotent; returns true if removed.
func (e *Engine) Delete(callsign string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.deleteLocked(callsign)
}

func (e *Engine) deleteLocked(callsign string) bool {
	cs := strings.ToUpper(strings.TrimSpace(callsign))
	if cs == "" {
		return false
	}
	if _, ok := e.aircraft[cs]; !ok {
		return false
	}
	delete(e.aircraft, cs)
	return true
}

// Get returns a snapshot of one aircraft.
func (e *Engine) Get(callsign string) (AircraftSnapshot, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ac, ok := e.aircraft[strings.ToUpper(strings.TrimSpace(callsign))]
	if !ok {
		return AircraftSnapshot{}, false
	}
	return ac.snapshot(), true
}

// Count returns the number of aircraft currently in the engine.
func (e *Engine) Count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.aircraft)
}

// Airport returns a pointer to the loaded airport (nil if none).
// Callers must not mutate surfaces; treat as read-only.
func (e *Engine) Airport() *Airport {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.airport
}

// Graph returns the taxi graph (nil if no airport).
func (e *Engine) Graph() *Graph {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.graph
}

// Snapshot returns a full engine snapshot with aircraft sorted by callsign.
func (e *Engine) Snapshot() EngineSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	snap := EngineSnapshot{
		Paused:   e.paused,
		Elapsed:  e.elapsed,
		ArrCount: e.arr,
		DepCount: e.dep,
	}
	if e.airport != nil {
		snap.ICAO = e.airport.ICAO
	}
	if len(e.aircraft) == 0 {
		return snap
	}
	keys := make([]string, 0, len(e.aircraft))
	for cs := range e.aircraft {
		keys = append(keys, cs)
	}
	sort.Strings(keys)
	snap.Aircraft = make([]AircraftSnapshot, 0, len(keys))
	for _, cs := range keys {
		snap.Aircraft = append(snap.Aircraft, e.aircraft[cs].snapshot())
	}
	return snap
}

// Ops returns session statistics (elapsed, arr/dep, ops/min).
func (e *Engine) Ops() OpsStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.opsLocked()
}

func (e *Engine) opsLocked() OpsStats {
	s := OpsStats{
		Elapsed:  e.elapsed,
		ArrCount: e.arr,
		DepCount: e.dep,
	}
	mins := e.elapsed.Minutes()
	if mins > 0 {
		s.OpsPerMin = float64(e.arr+e.dep) / mins
	}
	return s
}

// formatOpsMessage renders TWRTrainer-style ops/stats text.
func formatOpsMessage(s OpsStats) string {
	d := s.Elapsed
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	return fmt.Sprintf("Elapsed: %d:%02d:%02d  Arrivals: %d  Departures: %d  Ops/min: %.2f",
		h, m, sec, s.ArrCount, s.DepCount, s.OpsPerMin)
}

// nextSquawkLocked returns a 4-digit squawk and advances the sequence.
func (e *Engine) nextSquawkLocked(rules string) string {
	// VFR default 1200 when starting fresh; otherwise sequential IFR-style.
	if strings.EqualFold(rules, RulesVFR) || strings.EqualFold(rules, RulesDVFR) || strings.EqualFold(rules, RulesSVFR) {
		// Still unique-ish among engine aircraft: walk until free or fall through.
		for i := 0; i < 1000; i++ {
			code := fmt.Sprintf("%04d", e.sqkSeq%10000)
			e.sqkSeq++
			if e.sqkSeq > 7777 {
				e.sqkSeq = 2000
			}
			// Prefer 1200 for first VFR if free.
			if i == 0 {
				if !e.squawkInUseLocked("1200") {
					return "1200"
				}
			}
			if isSquawk(code) && !e.squawkInUseLocked(code) {
				return code
			}
		}
		return "1200"
	}
	for i := 0; i < 10000; i++ {
		code := fmt.Sprintf("%04d", e.sqkSeq%10000)
		e.sqkSeq++
		if e.sqkSeq > 7777 {
			e.sqkSeq = 2000
		}
		if isSquawk(code) && !e.squawkInUseLocked(code) {
			return code
		}
	}
	return "2000"
}

func (e *Engine) squawkInUseLocked(code string) bool {
	for _, ac := range e.aircraft {
		if ac.Squawk == code {
			return true
		}
	}
	return false
}

// generateCallsignLocked builds a unique callsign from airport airline lists
// or the registration prefix. Empty airport lists fall back to N-numbers.
func (e *Engine) generateCallsignLocked(engineType string) string {
	e.csSeq++
	n := e.csSeq
	apt := e.airport
	var prefixes []string
	switch strings.ToUpper(engineType) {
	case EngineJet:
		if apt != nil && apt.JetAirlines != "" {
			prefixes = splitCSV(apt.JetAirlines)
		}
	case EngineTurboprop:
		if apt != nil && apt.TurboAirlines != "" {
			prefixes = splitCSV(apt.TurboAirlines)
		}
		// fall back to jet list if turbo empty
		if len(prefixes) == 0 && apt != nil && apt.JetAirlines != "" {
			prefixes = splitCSV(apt.JetAirlines)
		}
	}

	if len(prefixes) > 0 {
		pref := prefixes[(n-1)%len(prefixes)]
		// 1–3 digit flight number for uniqueness
		num := (n-1)/len(prefixes) + 1
		for attempt := 0; attempt < 10000; attempt++ {
			cs := fmt.Sprintf("%s%d", pref, num+attempt)
			if _, exists := e.aircraft[cs]; !exists {
				return cs
			}
		}
	}

	// GA registration: prefix + digits (e.g. N12345)
	reg := defaultRegistration
	if apt != nil && apt.Registration != "" {
		reg = strings.ToUpper(apt.Registration)
	}
	for attempt := 0; attempt < 100000; attempt++ {
		cs := fmt.Sprintf("%s%05d", reg, (n+attempt)%100000)
		if _, exists := e.aircraft[cs]; !exists {
			return cs
		}
	}
	// Last resort: include sequence
	return fmt.Sprintf("%sX%d", reg, n)
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- Geometry helpers (stdlib math + geo.Distance) ---

// destinationPoint returns the point distM meters from (lat,lon) on bearingDeg
// (0=N, 90=E) using a spherical Earth model.
func destinationPoint(lat, lon, bearingDeg, distM float64) (float64, float64) {
	if distM == 0 {
		return lat, lon
	}
	const r = geo.EarthRadius
	δ := distM / r
	θ := bearingDeg * math.Pi / 180
	φ1 := lat * math.Pi / 180
	λ1 := lon * math.Pi / 180
	sinφ1 := math.Sin(φ1)
	cosφ1 := math.Cos(φ1)
	sinδ := math.Sin(δ)
	cosδ := math.Cos(δ)
	sinφ2 := sinφ1*cosδ + cosφ1*sinδ*math.Cos(θ)
	φ2 := math.Asin(sinφ2)
	y := math.Sin(θ) * sinδ * cosφ1
	x := cosδ - sinφ1*sinφ2
	λ2 := λ1 + math.Atan2(y, x)
	lat2 := φ2 * 180 / math.Pi
	lon2 := math.Mod(λ2*180/math.Pi+540, 360) - 180
	return lat2, lon2
}

// initialBearingDeg returns the initial great-circle bearing from A to B.
func initialBearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	Δλ := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(Δλ) * math.Cos(φ2)
	x := math.Cos(φ1)*math.Sin(φ2) - math.Sin(φ1)*math.Cos(φ2)*math.Cos(Δλ)
	θ := math.Atan2(y, x) * 180 / math.Pi
	return normalizeHeading(θ)
}

// runwayThreshold returns the threshold point and landing heading for a runway
// end designator (e.g. "19" or "33"). Combined names ("19/1") default to RwyA.
// Landing heading is along the runway centerline from the threshold toward the
// far end.
func runwayThreshold(s *Surface, end string) (threshold Point, hdg float64, ok bool) {
	if s == nil || s.Kind != SurfaceRunway || len(s.Points) < 2 {
		return Point{}, 0, false
	}
	end = strings.ToUpper(strings.TrimSpace(end))
	// Combined "A/B" (or full surface Name) → approach end A by default.
	if end == s.Name || end == s.RwyA+"/"+s.RwyB {
		end = s.RwyA
	}
	// Points are ordered RwyA → RwyB in .apt files.
	var thr, far Point
	var dispFt float64
	switch end {
	case s.RwyA:
		thr = s.Points[0]
		far = s.Points[len(s.Points)-1]
		dispFt = s.DispA
	case s.RwyB:
		thr = s.Points[len(s.Points)-1]
		far = s.Points[0]
		dispFt = s.DispB
	default:
		return Point{}, 0, false
	}
	hdg = initialBearingDeg(thr.Lat, thr.Lon, far.Lat, far.Lon)
	// Displaced threshold: move thr along landing heading by disp feet.
	if dispFt > 0 {
		// 1 foot = 0.3048 m
		thr.Lat, thr.Lon = destinationPoint(thr.Lat, thr.Lon, hdg, dispFt*0.3048)
	}
	return thr, hdg, true
}

// fieldReferencePoint is a rough airport center for bearing-style adds.
func fieldReferencePoint(apt *Airport) Point {
	if apt == nil || len(apt.Surfaces) == 0 {
		return Point{}
	}
	// Prefer midpoint of first runway; else average of first points of all surfaces.
	for i := range apt.Surfaces {
		s := &apt.Surfaces[i]
		if s.Kind == SurfaceRunway && len(s.Points) >= 2 {
			a := s.Points[0]
			b := s.Points[len(s.Points)-1]
			return Point{Lat: (a.Lat + b.Lat) / 2, Lon: (a.Lon + b.Lon) / 2}
		}
	}
	var sumLat, sumLon float64
	var n int
	for i := range apt.Surfaces {
		s := &apt.Surfaces[i]
		if len(s.Points) == 0 {
			continue
		}
		sumLat += s.Points[0].Lat
		sumLon += s.Points[0].Lon
		n++
	}
	if n == 0 {
		return Point{}
	}
	return Point{Lat: sumLat / float64(n), Lon: sumLon / float64(n)}
}

// approachAltitude returns MSL altitude for an aircraft distanceNM from threshold.
func approachAltitude(fieldElev, distanceNM float64) float64 {
	if distanceNM < 0 {
		distanceNM = 0
	}
	alt := fieldElev + glideslopeFtPerNM*distanceNM
	// Floor slightly above field.
	if alt < fieldElev+50 {
		alt = fieldElev + 50
	}
	return alt
}
