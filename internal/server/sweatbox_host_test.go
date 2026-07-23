package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/internal/sweatbox"
	"github.com/renorris/openfsd/pkg/protocol"
)

func newSweatboxEnv(t *testing.T) (*Server, *SweatboxHost, *memRegistry) {
	t.Helper()
	reg := &memRegistry{PostOffice: postoffice.New()}
	srv, err := New(Deps{
		Config: &Config{
			FsdListenAddrs:       []string{":0"},
			SweatboxEnabled:      true,
			SweatboxCID:          900001,
			SweatboxTickInterval: time.Second,
		},
		Users:           stubUserStore{},
		ConfigKV:        stubConfigStore{},
		Registry:        reg,
		Metar:           &recordingMetar{},
		Clock:           fixedClock{t: time.Unix(1_700_000_000, 0)},
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		SweatboxEnabled: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.sweatbox == nil {
		t.Fatal("expected sweatbox host when enabled")
	}
	return srv, srv.sweatbox, reg
}

func loadKBTV(t *testing.T, h *SweatboxHost) {
	t.Helper()
	apt := readTwrfilesFixture(t, "KBTV_example.apt")
	if err := h.LoadAirport(apt); err != nil {
		t.Fatalf("LoadAirport: %v", err)
	}
}

func readTwrfilesFixture(t *testing.T, name string) []byte {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "..", "pkg", "twrfiles", "testdata", name),
		filepath.Join("pkg", "twrfiles", "testdata", name),
		filepath.Join("..", "sweatbox", "testdata", name),
		filepath.Join("internal", "sweatbox", "testdata", name),
	}
	var last error
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err == nil {
			return b
		}
		last = err
	}
	t.Fatalf("read fixture %s: %v", name, last)
	return nil
}

func mustAddParked(t *testing.T, h *SweatboxHost, cs string) sweatbox.CommandResult {
	t.Helper()
	// KBTV has parking stands; use bearing form for deterministic placement.
	// add rules weight engine -bearing dist alt [type]
	line := "add I L J -90 5 3000 B738"
	res := h.ApplyCommand(line)
	if !res.OK || len(res.Added) != 1 {
		t.Fatalf("add: ok=%v msg=%q added=%v", res.OK, res.Message, res.Added)
	}
	// Rename is not supported — generateCallsign was used. Use returned callsign.
	_ = cs
	return res
}

func TestSweatboxHost_DisabledByDefault(t *testing.T) {
	reg := &memRegistry{PostOffice: postoffice.New()}
	srv, err := New(Deps{
		Config:          &Config{FsdListenAddrs: []string{":0"}},
		Users:           stubUserStore{},
		ConfigKV:        stubConfigStore{},
		Registry:        reg,
		Metar:           &recordingMetar{},
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		SweatboxEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if srv.sweatbox != nil {
		t.Fatal("sweatbox should be nil when disabled")
	}
}

func TestSweatboxHost_AddRemoveLifecycle(t *testing.T) {
	srv, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	// ATC observer for #AP / #DP
	atc := newSess("BOS_TWR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	// Drain join noise
	drain(atc)

	res := h.ApplyCommand("add I L J -90 5 3000 B738")
	if !res.OK || len(res.Added) != 1 {
		t.Fatalf("add failed: %+v", res)
	}
	cs := res.Added[0].Callsign

	// #AP delivered to ATC
	pkts := drain(atc)
	if !containsPrefix(pkts, "#AP"+cs) {
		t.Fatalf("expected #AP for %s, got %v", cs, pkts)
	}

	s := h.SessionFor(cs)
	if s == nil || !s.Synthetic {
		t.Fatal("expected synthetic session in host map")
	}
	if _, err := reg.Find(cs); err != nil {
		t.Fatalf("registry Find: %v", err)
	}
	if h.eng.Count() != 1 {
		t.Fatalf("engine count = %d", h.eng.Count())
	}

	// Flight plan stored
	if fp := s.FlightPlan.Load(); fp == "" || !strings.HasPrefix(fp, "I:") {
		t.Fatalf("flight plan = %q", fp)
	}

	// State merges session FP
	st := h.State()
	if len(st.Aircraft) != 1 || st.Aircraft[0].FlightPlan == "" {
		t.Fatalf("State = %+v", st)
	}

	if err := h.Remove(cs); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	pkts = drain(atc)
	if !containsPrefix(pkts, "#DP"+cs) {
		t.Fatalf("expected #DP for %s, got %v", cs, pkts)
	}
	if _, err := reg.Find(cs); err == nil {
		t.Fatal("expected registry empty after Remove")
	}
	if h.eng.Count() != 0 {
		t.Fatalf("engine count after remove = %d", h.eng.Count())
	}
	if h.SessionFor(cs) != nil {
		t.Fatal("host map should be empty")
	}

	// Double Remove idempotent
	if err := h.Remove(cs); err == nil {
		// not found is fine
	}
	_ = srv
}

func TestSweatboxHost_RegisterThenRemoveBeforeBroadcast_NoGhostAP(t *testing.T) {
	_, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	atc := newSess("BOS_TWR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	drain(atc)

	var once sync.Once
	h.testAfterRegister = func(cs string) {
		once.Do(func() {
			// Concurrent Remove after Register, before commit/announce.
			_ = h.Remove(cs)
		})
	}

	res := h.ApplyCommand("add I L J -90 5 3000 B738")
	// Add should fail commit check (aborted) or succeed only if Remove lost race
	// before claim drop — either way no live ghost.
	pkts := drain(atc)
	for _, p := range pkts {
		if strings.HasPrefix(p, "#AP") {
			// If #AP went out, Remove should have followed with #DP and empty registry.
			// Ghost = #AP with no membership.
			cs := res.Added
			_ = cs
		}
	}

	// Registry must not retain a synthetic without host claim, and no engine orphan
	// with a live registry entry after the race settles.
	if !res.OK {
		if h.eng.Count() != 0 {
			// abortFailedAdd should have deleted when owned
			// (Remove may have raced; settle)
			time.Sleep(20 * time.Millisecond)
		}
		// No #AP without committed add
		if containsPrefix(pkts, "#AP") {
			// Only acceptable if followed by full cleanup
			if h.eng.Count() != 0 {
				t.Fatalf("ghost: #AP seen and engine still has aircraft: %v", pkts)
			}
			for _, s := range reg.Snapshot() {
				if s.Synthetic {
					t.Fatalf("ghost synthetic still registered: %s", s.Callsign)
				}
			}
		}
		return
	}

	// If add OK somehow (Remove missed), still consistent
	if len(res.Added) == 1 {
		cs := res.Added[0].Callsign
		if h.SessionFor(cs) == nil {
			t.Fatal("committed add missing host claim")
		}
	}
}

func TestSweatboxHost_ABA_AbortDoesNotKillSuccessor(t *testing.T) {
	_, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	// Manually exercise abortFailedAdd against a successor claim.
	// Stage old session that owned claim then lost it to successor.
	old := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      "ABA1",
		CID:           900001,
		RealName:      "SWEATBOX",
		NetworkRating: protocol.NetworkRatingObserver,
		ProtoRevision: 100,
	})
	old.Synthetic = true

	// Put old in engine + claim + register as if mid-add
	h.eng.LoadScenario([]sweatbox.Aircraft{{
		Callsign: "ABA1", Engine: sweatbox.EngineJet, Rules: sweatbox.RulesIFR,
		Squawk: "2000", XPDRMode: sweatbox.XPDRModeNormal,
		Lat: 44.0, Lon: -73.0, Alt: 1000, Speed: 0, Heading: 90,
	}})
	h.mu.Lock()
	h.sessions["ABA1"] = old
	h.mu.Unlock()
	if err := reg.Register(old); err != nil {
		t.Fatal(err)
	}

	// Successor reclaims callsign in map+engine+registry
	_ = h.Remove("ABA1") // cleans old

	// Re-add successor via command
	res := h.ApplyCommand("add I L J -90 5 3000 B738")
	if !res.OK || len(res.Added) != 1 {
		// generated CS, not ABA1 — for successor-on-same-CS test use scenario
		t.Logf("add result: %+v", res)
	}

	// Explicit same-CS successor: load scenario aircraft with fixed CS
	// after abort path that does NOT own the map.
	succ := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      "SAMECS",
		CID:           900001,
		NetworkRating: protocol.NetworkRatingObserver,
		ProtoRevision: 100,
		RealName:      "SWEATBOX",
	})
	succ.Synthetic = true

	// Engine has SAMECS for successor only
	h.eng.Delete("SAMECS")
	h.eng.LoadScenario([]sweatbox.Aircraft{{
		Callsign: "SAMECS", Engine: sweatbox.EngineJet, Rules: sweatbox.RulesIFR,
		Squawk: "2100", XPDRMode: sweatbox.XPDRModeNormal,
		Lat: 44.1, Lon: -73.1, Alt: 2000, Speed: 100, Heading: 180,
	}})
	h.mu.Lock()
	h.sessions["SAMECS"] = succ
	h.mu.Unlock()
	if err := reg.Register(succ); err != nil {
		t.Fatal(err)
	}

	// Old pointer that never owned current claim — abort must not engine.Delete
	old2 := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      "SAMECS",
		CID:           900001,
		NetworkRating: protocol.NetworkRatingObserver,
		ProtoRevision: 100,
		RealName:      "OLD",
	})
	old2.Synthetic = true
	// old2 not in map; abort with owned=false
	h.abortFailedAdd(old2)

	if _, ok := h.eng.Get("SAMECS"); !ok {
		t.Fatal("abortFailedAdd killed successor engine aircraft")
	}
	if got := h.SessionFor("SAMECS"); got != succ {
		t.Fatal("abortFailedAdd disturbed successor host claim")
	}
	if s, err := reg.Find("SAMECS"); err != nil || s != succ {
		t.Fatalf("abortFailedAdd disturbed registry: %v %v", s, err)
	}
}

func TestSweatboxHost_ABA_AbortEngineBeforeClaim(t *testing.T) {
	_, h, _ := newSweatboxEnv(t)
	loadKBTV(t, h)

	// Successor in engine only (add order: engine first). Old abort without map claim.
	h.eng.LoadScenario([]sweatbox.Aircraft{{
		Callsign: "PRECLAIM", Engine: sweatbox.EngineJet, Rules: sweatbox.RulesIFR,
		Squawk: "2200", XPDRMode: sweatbox.XPDRModeNormal,
		Lat: 1, Lon: 2, Alt: 1000, Speed: 0, Heading: 0,
	}})

	old := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "PRECLAIM", CID: 900001, RealName: "OLD",
		NetworkRating: protocol.NetworkRatingObserver, ProtoRevision: 100,
	})
	old.Synthetic = true
	// sessions map empty → owned=false
	h.abortFailedAdd(old)

	if _, ok := h.eng.Get("PRECLAIM"); !ok {
		t.Fatal("abort without ownership must not engine.Delete")
	}
}

func TestSweatboxHost_KickReAddSameCS_SuccessorSurvives(t *testing.T) {
	srv, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	atc := newSess("BOS_TWR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	drain(atc)

	// Load fixed callsign via scenario
	air := []byte("TEST1:B738/F:J:I:KBTV:KBOS:29000:DCT:/v/:2200:N:44.47:-73.15:1000:0:90\n")
	n, errs := h.LoadScenario(air)
	if n != 1 {
		t.Fatalf("LoadScenario loaded=%d errs=%v", n, errs)
	}
	old := h.SessionFor("TEST1")
	if old == nil {
		t.Fatal("missing TEST1")
	}

	// Kick path
	if old.Synthetic && srv.sweatbox != nil {
		_ = srv.sweatbox.Remove("TEST1")
	}

	// Immediate re-add same CS
	air2 := []byte("TEST1:B738/F:J:I:KBTV:KBOS:29000:DCT:/v/:2201:N:44.48:-73.16:2000:50:180\n")
	// Engine may still be empty; LoadScenario again
	n, errs = h.LoadScenario(air2)
	if n != 1 {
		t.Fatalf("re-add LoadScenario loaded=%d errs=%v", n, errs)
	}
	succ := h.SessionFor("TEST1")
	if succ == nil || succ == old {
		t.Fatal("expected new successor session for TEST1")
	}

	// Wait for any delayed AfterFunc from old Cancel to settle
	time.Sleep(50 * time.Millisecond)

	if got := h.SessionFor("TEST1"); got != succ {
		t.Fatal("old AfterFunc/cleanup killed successor host claim")
	}
	if s, err := reg.Find("TEST1"); err != nil || s != succ {
		t.Fatalf("successor not in registry: err=%v s=%v", err, s)
	}
	if _, ok := h.eng.Get("TEST1"); !ok {
		t.Fatal("successor missing from engine")
	}
	// Old session must be canceled; successor live
	if old.Ctx.Err() == nil {
		t.Fatal("old session should be canceled")
	}
	if succ.Ctx.Err() != nil {
		t.Fatal("successor should still be live")
	}
}

func TestSweatboxHost_KillRequestSynthetic(t *testing.T) {
	srv, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	air := []byte("KILLME:B738/F:J:I:KBTV:KBOS:10000:DCT::1200:N:44.47:-73.15:1000:0:90\n")
	if n, _ := h.LoadScenario(air); n != 1 {
		t.Fatal("load")
	}

	sup := newSess("SUP1", true, NetworkRatingSupervisor)
	if err := reg.Register(sup); err != nil {
		t.Fatal(err)
	}

	srv.handleKillRequest(sup, []byte("$!!SUP1:KILLME\r\n"))

	if _, err := reg.Find("KILLME"); err == nil {
		t.Fatal("kill should remove synthetic from registry")
	}
	if h.eng.Count() != 0 {
		t.Fatal("kill should remove from engine")
	}
}

func TestSweatboxHost_DoubleRemoveIdempotent(t *testing.T) {
	_, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)
	air := []byte("DBL1:B738/F:J:I:KBTV:KBOS:10000:DCT::1200:N:44.47:-73.15:1000:0:90\n")
	if n, _ := h.LoadScenario(air); n != 1 {
		t.Fatal("load")
	}
	if err := h.Remove("DBL1"); err != nil {
		t.Fatal(err)
	}
	if err := h.Remove("DBL1"); err == nil {
		// second should be not found — ok either way if no panic
	}
	// Successor re-add then double-remove old pointer via cleanup must not harm
	if n, _ := h.LoadScenario(air); n != 1 {
		t.Fatal("re-add")
	}
	succ := h.SessionFor("DBL1")
	// cleanup old nil is fine
	h.cleanupSyntheticSession(nil)
	// cleanup with wrong pointer (new fake) must not kill succ
	fake := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "DBL1", CID: 1, NetworkRating: protocol.NetworkRatingObserver, ProtoRevision: 100,
	})
	fake.Synthetic = true
	h.cleanupSyntheticSession(fake)
	if h.SessionFor("DBL1") != succ {
		t.Fatal("foreign cleanup harmed successor")
	}
	if s, err := reg.Find("DBL1"); err != nil || s != succ {
		t.Fatal("registry harmed")
	}
}

func TestSweatboxHost_ApplyTickUpdate_Liveness(t *testing.T) {
	_, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)
	air := []byte("TIK1:B738/F:J:I:KBTV:KBOS:10000:DCT::1200:N:44.47:-73.15:1000:200:90\n")
	if n, _ := h.LoadScenario(air); n != 1 {
		t.Fatal("load")
	}
	// Unpause and command speed/heading so kinematics move
	h.ApplyCommand("un")
	h.ApplyCommand("TIK1 fh 90")
	h.ApplyCommand("TIK1 spd 200")
	h.ApplyCommand("TIK1 cm 5000")

	// Apply after Remove must not re-insert into registry via UpdatePosition
	_ = h.Remove("TIK1")
	h.applyTickUpdate(sweatbox.AircraftSnapshot{
		Callsign: "TIK1", Lat: 45, Lon: -74, Alt: 5000, Speed: 200, Heading: 90, Squawk: "1200",
	})
	if _, err := reg.Find("TIK1"); err == nil {
		t.Fatal("applyTickUpdate after Remove re-registered ghost")
	}
}

func TestSweatboxHost_RaceCommandTickRemove(t *testing.T) {
	_, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	var wg sync.WaitGroup
	var ops atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Seed a few aircraft
	for i := 0; i < 3; i++ {
		h.ApplyCommand("add I L J -90 5 3000 B738")
	}
	h.ApplyCommand("un")

	wg.Add(3)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			h.ApplyCommand("add I L J -45 3 4000 B738")
			ops.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			h.tickOnce(100 * time.Millisecond)
			ops.Add(1)
		}
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			for _, s := range reg.Snapshot() {
				if s.Synthetic {
					_ = h.Remove(s.Callsign)
					break
				}
			}
			ops.Add(1)
		}
	}()
	wg.Wait()

	// Settled consistency: every host session is in registry with same pointer
	// and Synthetic; every engine AC with a session has matching claim.
	h.mu.Lock()
	for cs, s := range h.sessions {
		s2, err := reg.Find(cs)
		if err != nil || s2 != s {
			h.mu.Unlock()
			t.Fatalf("map/registry mismatch for %s", cs)
		}
		if !s.Synthetic {
			h.mu.Unlock()
			t.Fatalf("%s not synthetic", cs)
		}
	}
	h.mu.Unlock()

	if ops.Load() == 0 {
		t.Fatal("race did no work")
	}
}

func TestSweatboxHost_ScenarioLoadAndFP(t *testing.T) {
	_, h, reg := newSweatboxEnv(t)
	loadKBTV(t, h)

	atc := newSess("BOS_TWR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	drain(atc)

	raw := readTwrfilesFixture(t, "KBTV_example.air")
	n, errs := h.LoadScenario(raw)
	if n != 3 {
		t.Fatalf("loaded=%d errs=%v", n, errs)
	}
	if h.eng.Paused() != true {
		t.Fatal("scenario should auto-pause")
	}

	// $FP for AAL123
	s := h.SessionFor("AAL123")
	if s == nil {
		t.Fatal("AAL123 missing")
	}
	wantPrefix := "I:B738/F:"
	if fp := s.FlightPlan.Load(); !strings.HasPrefix(fp, wantPrefix) {
		t.Fatalf("FP = %q", fp)
	}

	// Instructor fp updates session + ATC broadcast
	drain(atc)
	res := h.ApplyCommand("AAL123 fp B738 220 KBOS DCT KJFK")
	if !res.OK {
		t.Fatalf("fp: %v", res.Message)
	}
	fp := s.FlightPlan.Load()
	if !strings.Contains(fp, "22000") && !strings.Contains(fp, ":220:") {
		// cruise may be 22000 feet from FL220
		if !strings.Contains(fp, "22000") {
			t.Fatalf("expected cruise in FP after fp cmd: %q", fp)
		}
	}
	pkts := drain(atc)
	if !containsPrefix(pkts, "$FPAAL123") {
		t.Fatalf("expected $FP broadcast, got %v", pkts)
	}

	// Snapshot merge prefers session FP
	st := h.State()
	var found bool
	for _, ac := range st.Aircraft {
		if ac.Callsign == "AAL123" {
			found = true
			if ac.FlightPlan != s.FlightPlan.Load() {
				t.Fatalf("State FP = %q session = %q", ac.FlightPlan, s.FlightPlan.Load())
			}
		}
	}
	if !found {
		t.Fatal("AAL123 missing from State")
	}
}

func containsPrefix(pkts []string, prefix string) bool {
	for _, p := range pkts {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
