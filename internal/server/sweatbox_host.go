package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/internal/sweatbox"
	"github.com/renorris/openfsd/pkg/protocol"
)

// Pilot visibility range for synthetic position updates (matches handlePilotPosition).
const sweatboxPilotVisRangeM = 50.0 * 1852.0

const (
	defaultSweatboxCID      = 900001
	defaultSweatboxInterval = time.Second
	minSweatboxInterval     = 200 * time.Millisecond
	maxSweatboxInterval     = 10 * time.Second
)

// Host-level errors for instructor / test callers.
var (
	errSweatboxNotFound          = errors.New("sweatbox: callsign not found")
	errSweatboxAbortedConcurrent = errors.New("sweatbox: add aborted by concurrent remove")
	errSweatboxDisabled          = errors.New("sweatbox: disabled")
)

// SweatboxHost couples the pure sweatbox.Engine to FSD sessions/registry/wire.
//
// Lock order: never hold eng.mu (inside engine methods) and host.mu together.
// Never hold host.mu across registry.Send / broadcast.
// wireMu serializes host-originated Search/All fan-out so concurrent tick +
// command do not race Session.ClosestVelocityClientDistance (non-atomic).
// wireMu is never held while taking host.mu (order: wireMu then host.mu is OK
// only if host.mu is not already held; prefer release host.mu before wire work).
type SweatboxHost struct {
	srv *Server
	eng *sweatbox.Engine

	mu        sync.Mutex
	sessions  map[string]*session.Session // callsign → session (claim before Register)
	afterStop map[*session.Session]func() bool

	// wireMu serializes UpdatePosition + broadcast fan-out from this host.
	wireMu sync.Mutex

	interval time.Duration
	cid      int

	// testAfterRegister is invoked after registry.Register succeeds and before
	// the post-Register commit check. Tests inject concurrent Remove here.
	testAfterRegister func(callsign string)
}

func newSweatboxHost(srv *Server) *SweatboxHost {
	interval := defaultSweatboxInterval
	cid := defaultSweatboxCID
	if srv != nil && srv.cfg != nil {
		if srv.cfg.SweatboxCID > 0 {
			cid = srv.cfg.SweatboxCID
		}
		interval = resolveTickInterval(srv.cfg)
	}
	return &SweatboxHost{
		srv:       srv,
		eng:       sweatbox.NewEngine(),
		sessions:  make(map[string]*session.Session),
		afterStop: make(map[*session.Session]func() bool),
		interval:  interval,
		cid:       cid,
	}
}

func resolveTickInterval(cfg *Config) time.Duration {
	if cfg == nil {
		return defaultSweatboxInterval
	}
	if cfg.SweatboxTickHz > 0 {
		return clampTickInterval(time.Duration(float64(time.Second) / cfg.SweatboxTickHz))
	}
	if cfg.SweatboxTickInterval > 0 {
		return clampTickInterval(cfg.SweatboxTickInterval)
	}
	return defaultSweatboxInterval
}

func clampTickInterval(d time.Duration) time.Duration {
	if d < minSweatboxInterval {
		return minSweatboxInterval
	}
	if d > maxSweatboxInterval {
		return maxSweatboxInterval
	}
	return d
}

// Engine returns the pure simulator engine (tests / HTTP later).
func (h *SweatboxHost) Engine() *sweatbox.Engine {
	if h == nil {
		return nil
	}
	return h.eng
}

// Run ticks the engine until ctx is cancelled, then tears down all synthetics.
func (h *SweatboxHost) Run(ctx context.Context) {
	if h == nil {
		return
	}
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			h.shutdownAll()
			return
		case <-ticker.C:
			h.tickOnce(h.interval)
		}
	}
}

// tickOnce advances the engine by dt and applies Updates/Deletes to the wire.
// Exported for tests that need deterministic apply without waiting on the ticker.
func (h *SweatboxHost) tickOnce(dt time.Duration) {
	result := h.eng.Tick(dt)
	for i := range result.Updates {
		h.applyTickUpdate(result.Updates[i])
	}
	for _, cs := range result.Deletes {
		_ = h.Remove(cs)
	}
}

// AirportLoadResult is the structured outcome of loading .apt text (HTTP / tests).
type AirportLoadResult struct {
	ICAO     string   `json:"icao"`
	Surfaces int      `json:"surfaces"`
	Errors   []string `json:"errors"`
}

// LoadAirport parses .apt text and installs it on the engine.
// Does not remove existing aircraft; use LoadAirportReplace for that flow.
func (h *SweatboxHost) LoadAirport(text []byte) error {
	res, err := h.LoadAirportResult(text, false)
	if err != nil {
		return err
	}
	if res.ICAO == "" {
		if len(res.Errors) > 0 {
			return fmt.Errorf("sweatbox: invalid airport: %s", res.Errors[0])
		}
		return errors.New("sweatbox: invalid airport")
	}
	return nil
}

// LoadAirportResult parses .apt text and installs it. When replace is true and
// aircraft are present, RemoveAll runs first so the airport can be swapped.
// Soft parse issues are returned in Errors; hard failures return a non-nil error.
func (h *SweatboxHost) LoadAirportResult(text []byte, replace bool) (AirportLoadResult, error) {
	if h == nil {
		return AirportLoadResult{}, errSweatboxDisabled
	}
	apt, errs := sweatbox.ParseAPT(string(text))
	res := AirportLoadResult{
		ICAO:     apt.ICAO,
		Surfaces: len(apt.Surfaces),
		Errors:   errs,
	}
	if apt.ICAO == "" {
		if len(errs) == 0 {
			res.Errors = []string{"ICAO code missing"}
		}
		return res, errors.New("sweatbox: invalid airport")
	}
	if replace && h.eng.Count() > 0 {
		h.RemoveAll()
	}
	if err := h.eng.LoadAirport(&apt); err != nil {
		return res, err
	}
	return res, nil
}

// LoadScenario parses .air text, loads into the engine (best-effort), and
// registers a synthetic session for each successfully loaded callsign.
// Auto-pauses (engine.LoadScenario). Returns count of wire-registered aircraft.
func (h *SweatboxHost) LoadScenario(text []byte) (loaded int, errs []error) {
	if h == nil {
		return 0, []error{errSweatboxDisabled}
	}
	rows, parseErrs := sweatbox.ParseAIR(string(text))
	for _, e := range parseErrs {
		errs = append(errs, errors.New(e))
	}
	loadedCS, engErrs := h.eng.LoadScenario(rows)
	for _, e := range engErrs {
		errs = append(errs, errors.New(e))
	}
	for _, cs := range loadedCS {
		snap, ok := h.eng.Get(cs)
		if !ok {
			errs = append(errs, fmt.Errorf("sweatbox: engine missing %s after load", cs))
			continue
		}
		if err := h.registerFromSnapshot(snap); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", cs, err))
			continue
		}
		loaded++
	}
	return loaded, errs
}

// ApplyCommand runs an instructor text command on the engine, then couples
// Added/Deleted callsigns to the registry and syncs wire-visible session fields.
func (h *SweatboxHost) ApplyCommand(line string) sweatbox.CommandResult {
	return h.ApplyCommandSelected("", line)
}

// ApplyCommandSelected is ApplyCommand with a UI-selected callsign for
// aircraft-scoped verbs that omit an embedded callsign (e.g. "fp B738 …").
func (h *SweatboxHost) ApplyCommandSelected(selectedCS, line string) sweatbox.CommandResult {
	if h == nil {
		return sweatbox.CommandResult{OK: false, Message: errSweatboxDisabled.Error()}
	}
	result := h.eng.Command(selectedCS, line)
	if !result.OK {
		return result
	}

	// Deletes: engine already removed domain state; tear down wire sessions.
	for _, cs := range result.Deleted {
		_ = h.Remove(cs)
	}

	// Adds: engine already has domain state; claim + Register + announce.
	var kept []sweatbox.AircraftSnapshot
	for _, ac := range result.Added {
		if err := h.registerFromSnapshot(ac); err != nil {
			return sweatbox.CommandResult{
				OK:      false,
				Message: fmt.Sprintf("Failed to register %s: %v", ac.Callsign, err),
			}
		}
		kept = append(kept, ac)
	}
	result.Added = kept

	// fp/vp (and other mutations): refresh session atomics from engine.
	if len(result.Deleted) == 0 && len(result.Added) == 0 {
		cs, broadcastFP := commandWireSync(line)
		if cs == "" {
			cs = strings.ToUpper(strings.TrimSpace(selectedCS))
			if cs != "" {
				broadcastFP = commandBroadcastFP(line)
			}
		}
		if cs != "" {
			if snap, ok := h.eng.Get(cs); ok {
				h.syncSessionWire(cs, snap, broadcastFP)
			}
		}
	}
	return result
}

// Pause freezes simulation motion (elapsed stops accumulating on tick).
func (h *SweatboxHost) Pause() {
	if h == nil {
		return
	}
	h.eng.Pause()
}

// Unpause resumes simulation motion.
func (h *SweatboxHost) Unpause() {
	if h == nil {
		return
	}
	h.eng.Unpause()
}

// Ops returns session statistics for HTTP / instructor UI.
func (h *SweatboxHost) Ops() sweatbox.OpsStats {
	if h == nil {
		return sweatbox.OpsStats{}
	}
	return h.eng.Ops()
}

// commandWireSync extracts a target callsign and whether to rebroadcast $FP.
func commandWireSync(line string) (callsign string, broadcastFP bool) {
	fields := commandFields(line)
	if len(fields) == 0 {
		return "", false
	}
	// Forms: "AAL123 fp …" / "AAL123, del" / global "add …" / "p"
	first := strings.ToUpper(fields[0])
	switch strings.ToLower(fields[0]) {
	case "add", "p", "pause", "un", "unp", "unpause", "u", "up", "ops", "stats":
		return "", false
	}
	if len(fields) < 2 {
		return first, false
	}
	verb := strings.ToLower(fields[1])
	switch verb {
	case "fp", "vp", "remarks":
		return first, true
	case "del", "pos", "ph", "sq", "sqi", "sn", "ss", "id",
		"taxi", "hold", "res", "cross", "cto", "ctoc", "cancel", "can",
		"ctomlt", "ctomrt", "nostop", "nohold",
		"fh", "fhn", "tr", "tl", "fph", "fch", "cm", "dm",
		"spd", "speed", "slow", "sln", "sl", "ds", "is",
		"ctopp", "land", "hs":
		return first, false
	default:
		return "", false
	}
}

// commandBroadcastFP reports whether a verb-first line (selected callsign form)
// should rebroadcast $FP.
func commandBroadcastFP(line string) bool {
	fields := commandFields(line)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "fp", "vp", "remarks":
		return true
	default:
		return false
	}
}

func commandFields(line string) []string {
	return strings.Fields(strings.Map(func(r rune) rune {
		if r == ',' {
			return ' '
		}
		return r
	}, strings.TrimSpace(line)))
}

// Remove tears down a synthetic by callsign (map lookup, else registry Find).
// Pointer-scoped cleanup is successor-safe (ABA).
func (h *SweatboxHost) Remove(callsign string) error {
	if h == nil {
		return errSweatboxDisabled
	}
	cs := strings.ToUpper(strings.TrimSpace(callsign))
	if cs == "" {
		return errSweatboxNotFound
	}

	h.mu.Lock()
	s := h.sessions[cs]
	h.mu.Unlock()

	if s == nil {
		sReg, err := h.srv.registry.Find(cs)
		if err != nil || sReg == nil || !sReg.Synthetic {
			return errSweatboxNotFound
		}
		s = sReg
	}
	h.cleanupSyntheticSession(s)
	return nil
}

// RemoveAll removes every synthetic currently claimed or present in the engine.
func (h *SweatboxHost) RemoveAll() {
	if h == nil {
		return
	}
	snap := h.eng.Snapshot()
	for _, ac := range snap.Aircraft {
		_ = h.Remove(ac.Callsign)
	}
	h.mu.Lock()
	left := make([]*session.Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		left = append(left, s)
	}
	h.mu.Unlock()
	for _, s := range left {
		h.cleanupSyntheticSession(s)
	}
}

func (h *SweatboxHost) shutdownAll() {
	h.RemoveAll()
}

// ---------------------------------------------------------------------------
// Dual bookkeeping: registerFromSnapshot / abort / cleanup
// ---------------------------------------------------------------------------

// registerFromSnapshot assumes the aircraft is already in the engine.
// Claim → Register → post-Register commit check → SenderWorker → AfterFunc → #AP/@/$FP.
func (h *SweatboxHost) registerFromSnapshot(ac sweatbox.AircraftSnapshot) error {
	cs := strings.ToUpper(strings.TrimSpace(ac.Callsign))
	if cs == "" {
		return errors.New("sweatbox: empty callsign")
	}
	ac.Callsign = cs

	s := h.buildSession(ac)

	// 3. Claim before registry visibility.
	h.mu.Lock()
	if existing := h.sessions[cs]; existing != nil {
		h.mu.Unlock()
		s.Cancel()
		h.eng.Delete(cs)
		return ErrCallsignInUse
	}
	h.sessions[cs] = s
	h.mu.Unlock()

	// 4. Register
	if err := h.srv.registry.Register(s); err != nil {
		// Still owns claim → abortFailedAdd (engine.Delete).
		h.abortFailedAdd(s)
		return err
	}

	// Test hook: concurrent Remove between Register and commit.
	if h.testAfterRegister != nil {
		h.testAfterRegister(cs)
	}

	// 5. Post-Register commit check (mandatory).
	h.mu.Lock()
	claimOK := h.sessions[cs] == s
	h.mu.Unlock()
	if !claimOK || s.Ctx.Err() != nil {
		h.abortFailedAdd(s)
		return errSweatboxAbortedConcurrent
	}

	// 6–7. SenderWorker + AfterFunc only after commit.
	go s.SenderWorker()
	stop := context.AfterFunc(s.Ctx, func() {
		// POINTER-SCOPED only — never callsign-scoped teardown.
		h.cleanupSyntheticSession(s)
	})
	h.setAfterFuncStop(s, stop)

	// 8. Announce (no host.mu held; wireMu serializes fan-out).
	h.wireMu.Lock()
	h.srv.broadcastAddPacket(s)
	h.broadcastPositionLocked(s, ac)
	h.broadcastFlightPlanLocked(s, ac)
	h.wireMu.Unlock()
	return nil
}

// buildSession constructs a Synthetic pilot session from an aircraft snapshot.
func (h *SweatboxHost) buildSession(ac sweatbox.AircraftSnapshot) *session.Session {
	now := h.srv.clock.Now()
	s := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:         ac.Callsign,
		CID:              h.cid,
		RealName:         "SWEATBOX",
		NetworkRating:    protocol.NetworkRatingObserver,
		MaxNetworkRating: protocol.NetworkRatingObserver,
		ProtoRevision:    100,
		LoginTime:        now,
		IsAtc:            false,
		ClientID:         0,
	})
	s.Synthetic = true
	s.FlightPlan.Store(encodeFlightPlanInfo(ac))
	s.Transponder.Store(ac.Squawk)
	s.Altitude.Store(int32(ac.Alt))
	s.Groundspeed.Store(int32(ac.Speed))
	s.Heading.Store(int32(ac.Heading))
	s.SetLatLon(ac.Lat, ac.Lon)
	s.VisRange.Store(sweatboxPilotVisRangeM)
	s.LastUpdated.Store(now)
	return s
}

// abortFailedAdd abandons an uncommitted add (pointer-scoped).
// No AfterFunc installed yet; no #AP was broadcast. Release without #DP.
func (h *SweatboxHost) abortFailedAdd(s *session.Session) {
	if s == nil {
		return
	}
	cs := s.Callsign
	h.stopAfterFunc(s) // should be none pre-commit

	h.mu.Lock()
	owned := h.sessions[cs] == s
	if owned {
		delete(h.sessions, cs)
	}
	h.mu.Unlock()

	if sReg, err := h.srv.registry.Find(cs); err == nil && sReg == s && s.Synthetic {
		h.srv.registry.Release(s)
	}
	s.Cancel()
	if owned {
		h.eng.Delete(cs)
	}
}

// cleanupSyntheticSession tears down THIS session only (successor-safe).
// Used by intentional Remove (after resolving s) and AfterFunc body.
func (h *SweatboxHost) cleanupSyntheticSession(s *session.Session) {
	if s == nil {
		return
	}
	cs := s.Callsign

	// 0. Stop AfterFunc before Cancel so intentional Remove does not re-enter.
	h.stopAfterFunc(s)

	// 1. Drop map claim iff we still own it.
	h.mu.Lock()
	owned := h.sessions[cs] == s
	if owned {
		delete(h.sessions, cs)
	}
	h.mu.Unlock()

	// 2. #DP + Release only if registry still points at THIS session.
	if sReg, err := h.srv.registry.Find(cs); err == nil && sReg == s && s.Synthetic {
		h.wireMu.Lock()
		h.srv.broadcastDisconnectPacket(s)
		h.wireMu.Unlock()
		h.srv.registry.Release(s)
	}

	// 3. Cancel (AfterFunc already stopped on intentional path).
	s.Cancel()

	// 4. engine.Delete only if owned at entry.
	if owned {
		h.eng.Delete(cs)
	}
}

func (h *SweatboxHost) stopAfterFunc(s *session.Session) {
	h.mu.Lock()
	stop := h.afterStop[s]
	delete(h.afterStop, s)
	h.mu.Unlock()
	if stop != nil {
		// Ignore return; if we are inside AfterFunc, Stop is a no-op/false.
		stop()
	}
}

func (h *SweatboxHost) setAfterFuncStop(s *session.Session, stop func() bool) {
	h.mu.Lock()
	h.afterStop[s] = stop
	h.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Tick apply + wire helpers
// ---------------------------------------------------------------------------

func (h *SweatboxHost) applyTickUpdate(ac sweatbox.AircraftSnapshot) {
	cs := ac.Callsign

	h.mu.Lock()
	s := h.sessions[cs]
	h.mu.Unlock()
	if s == nil {
		return // concurrent Remove dropped claim
	}

	s2, err := h.srv.registry.Find(cs)
	if err != nil || s2 != s || !s.Synthetic {
		// Released or replaced — never UpdatePosition a released session.
		return
	}

	s.SetLatLon(ac.Lat, ac.Lon)
	s.Altitude.Store(int32(ac.Alt + 0.5))
	s.Groundspeed.Store(int32(ac.Speed + 0.5))
	s.Heading.Store(int32(ac.Heading + 0.5))
	s.Transponder.Store(ac.Squawk)
	s.LastUpdated.Store(h.srv.clock.Now())

	h.wireMu.Lock()
	// Re-check under wireMu so we do not UpdatePosition after concurrent Release.
	s2, err = h.srv.registry.Find(cs)
	if err != nil || s2 != s || !s.Synthetic {
		h.wireMu.Unlock()
		return
	}
	h.srv.registry.UpdatePosition(s, [2]float64{ac.Lat, ac.Lon}, sweatboxPilotVisRangeM)
	h.broadcastPositionLocked(s, ac)
	h.wireMu.Unlock()
}

// broadcastPositionLocked requires wireMu held.
func (h *SweatboxHost) broadcastPositionLocked(s *session.Session, ac sweatbox.AircraftSnapshot) {
	mode := mapXPDRMode(ac)
	pkt := protocol.PilotPosition{
		TransponderMode:    mode,
		Callsign:           s.Callsign,
		TransponderCode:    ac.Squawk,
		NetworkRating:      s.NetworkRating,
		Latitude:           ac.Lat,
		Longitude:          ac.Lon,
		TrueAltitude:       int(ac.Alt + 0.5),
		Groundspeed:        int(ac.Speed + 0.5),
		PitchBankHeading:   packPitchBankHeading(0, 0, ac.Heading),
		AltitudeCorrection: 0,
	}.Marshal()
	s.LastUpdated.Store(h.srv.clock.Now())
	broadcastRanged(h.srv.registry, s, pkt)
}

// broadcastFlightPlanLocked requires wireMu held.
func (h *SweatboxHost) broadcastFlightPlanLocked(s *session.Session, ac sweatbox.AircraftSnapshot) {
	info := encodeFlightPlanInfo(ac)
	s.FlightPlan.Store(info)
	pkt := buildFileFlightplanPacket(s.Callsign, "*A", info)
	broadcastAllATC(h.srv.registry, s, []byte(pkt))
}

func (h *SweatboxHost) syncSessionWire(cs string, ac sweatbox.AircraftSnapshot, broadcastFP bool) {
	h.mu.Lock()
	s := h.sessions[cs]
	h.mu.Unlock()
	if s == nil {
		return
	}
	s2, err := h.srv.registry.Find(cs)
	if err != nil || s2 != s || !s.Synthetic {
		return
	}
	s.SetLatLon(ac.Lat, ac.Lon)
	s.Altitude.Store(int32(ac.Alt + 0.5))
	s.Groundspeed.Store(int32(ac.Speed + 0.5))
	s.Heading.Store(int32(ac.Heading + 0.5))
	s.Transponder.Store(ac.Squawk)
	s.LastUpdated.Store(h.srv.clock.Now())
	if broadcastFP {
		h.wireMu.Lock()
		h.broadcastFlightPlanLocked(s, ac)
		h.wireMu.Unlock()
	}
}

// mapXPDRMode maps sweatbox XPDR mode (+ ident) to wire @ mode char.
func mapXPDRMode(ac sweatbox.AircraftSnapshot) string {
	if ac.Ident {
		return "Y"
	}
	switch ac.XPDRMode {
	case sweatbox.XPDRModeStandby:
		return "S"
	default:
		return "N"
	}
}

// ---------------------------------------------------------------------------
// Snapshot (merged engine + session wire fields)
// ---------------------------------------------------------------------------

// SweatboxAircraftJSON is one aircraft in State(), with session wire overrides.
type SweatboxAircraftJSON struct {
	Callsign    string  `json:"callsign"`
	Type        string  `json:"type"`
	Rules       string  `json:"rules"`
	Squawk      string  `json:"squawk"`
	XPDRMode    string  `json:"xpdr_mode"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Alt         float64 `json:"alt"`
	Speed       float64 `json:"speed"`
	Heading     float64 `json:"heading"`
	Status      string  `json:"status"`
	Instruction string  `json:"instruction"`
	// FlightPlan is the live wire info section from the session (ATC $AM wins).
	FlightPlan string `json:"flight_plan"`
	Dep        string `json:"dep"`
	Arr        string `json:"arr"`
	CruiseAlt  int    `json:"cruise_alt"`
	Route      string `json:"route"`
	Remarks    string `json:"remarks"`
}

// SweatboxStateJSON is the instructor/UI snapshot (no HTTP surface in PR 6).
type SweatboxStateJSON struct {
	ICAO     string                 `json:"icao"`
	Paused   bool                   `json:"paused"`
	Elapsed  float64                `json:"elapsed_sec"`
	ArrCount int                    `json:"arr_count"`
	DepCount int                    `json:"dep_count"`
	Aircraft []SweatboxAircraftJSON `json:"aircraft"`
}

// State merges engine domain snapshot with session FlightPlan and wire atomics.
func (h *SweatboxHost) State() SweatboxStateJSON {
	if h == nil {
		return SweatboxStateJSON{Aircraft: []SweatboxAircraftJSON{}}
	}
	eng := h.eng.Snapshot()
	out := SweatboxStateJSON{
		ICAO:     eng.ICAO,
		Paused:   eng.Paused,
		Elapsed:  eng.Elapsed.Seconds(),
		ArrCount: eng.ArrCount,
		DepCount: eng.DepCount,
		Aircraft: make([]SweatboxAircraftJSON, 0, len(eng.Aircraft)),
	}
	for _, ac := range eng.Aircraft {
		row := SweatboxAircraftJSON{
			Callsign:    ac.Callsign,
			Type:        ac.Type,
			Rules:       ac.Rules,
			Squawk:      ac.Squawk,
			XPDRMode:    ac.XPDRMode,
			Lat:         ac.Lat,
			Lon:         ac.Lon,
			Alt:         ac.Alt,
			Speed:       ac.Speed,
			Heading:     ac.Heading,
			Status:      ac.Status,
			Instruction: ac.Instruction,
			Dep:         ac.Dep,
			Arr:         ac.Arr,
			CruiseAlt:   ac.CruiseAlt,
			Route:       ac.Route,
			Remarks:     ac.Remarks,
			FlightPlan:  encodeFlightPlanInfo(ac),
		}
		// Session is source of truth for wire-visible plan / position columns.
		h.mu.Lock()
		s := h.sessions[ac.Callsign]
		h.mu.Unlock()
		if s != nil {
			if fp := s.FlightPlan.Load(); fp != "" {
				row.FlightPlan = fp
			}
			ll := s.LatLon()
			row.Lat = ll[0]
			row.Lon = ll[1]
			row.Alt = float64(s.Altitude.Load())
			row.Speed = float64(s.Groundspeed.Load())
			row.Heading = float64(s.Heading.Load())
			if xpdr := s.Transponder.Load(); xpdr != "" {
				row.Squawk = xpdr
			}
		}
		out.Aircraft = append(out.Aircraft, row)
	}
	return out
}

// SessionFor returns the host-claimed session for callsign (tests).
func (h *SweatboxHost) SessionFor(callsign string) *session.Session {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[strings.ToUpper(strings.TrimSpace(callsign))]
}
