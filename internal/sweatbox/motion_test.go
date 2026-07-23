package sweatbox

import (
	"math"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

// --- Pause freeze -----------------------------------------------------------

func TestTickAircraft_UnknownStatusVectors(t *testing.T) {
	// default branch of tickAircraftLocked: unknown status still honors air vectors.
	e := NewEngine()
	e.mu.Lock()
	ac := &SimAircraft{
		Callsign:          "X",
		Status:            "Weird",
		Speed:             100,
		HasDesiredHeading: true,
		DesiredHeading:    90,
		Lat:               40, Lon: -70, Alt: 3000, Heading: 0,
	}
	changed, del := e.tickAircraftLocked(ac, 1.0)
	e.mu.Unlock()
	if del {
		t.Fatal("should not delete")
	}
	if !changed {
		t.Fatal("expected position/heading change from air vectors")
	}
	if ac.Heading <= 0 || ac.Heading > 90 {
		t.Fatalf("heading should turn toward 90 from 0, got %v", ac.Heading)
	}
	// Stationary unknown without vectors: pure no-op.
	e.mu.Lock()
	ac2 := &SimAircraft{Callsign: "Y", Status: "Weird", Speed: 0, Lat: 1, Lon: 2, Heading: 45}
	changed2, del2 := e.tickAircraftLocked(ac2, 1.0)
	e.mu.Unlock()
	if del2 || changed2 {
		t.Fatalf("noop want changed=false del=false, got changed=%v del=%v", changed2, del2)
	}
	if ac2.Heading != 45 || ac2.Lat != 1 || ac2.Lon != 2 {
		t.Fatalf("state mutated: %+v", ac2)
	}
}

func TestTick_PausedFreezesMotion(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Engine starts paused.
	before, _ := e.Get(cs)
	r := e.Tick(5 * time.Second)
	if len(r.Updates) != 0 || len(r.Deletes) != 0 {
		t.Fatalf("paused tick should return empty: %+v", r)
	}
	after, _ := e.Get(cs)
	if after.Lat != before.Lat || after.Lon != before.Lon || after.Alt != before.Alt || after.Heading != before.Heading {
		t.Error("paused tick must not move aircraft")
	}
	if e.Elapsed() != 0 {
		t.Errorf("paused elapsed = %v", e.Elapsed())
	}

	// Unpause, move, re-pause freezes again.
	e.Unpause()
	e.Command(cs, "fh 090")
	e.Tick(time.Second)
	mid, _ := e.Get(cs)
	e.Pause()
	e.Tick(10 * time.Second)
	end, _ := e.Get(cs)
	if end.Lat != mid.Lat || end.Lon != mid.Lon || end.Heading != mid.Heading {
		t.Error("re-pause must freeze motion")
	}
	if e.Elapsed() != time.Second {
		t.Errorf("elapsed after re-pause = %v, want 1s", e.Elapsed())
	}
}

// --- Taxi progress ----------------------------------------------------------

func TestTick_TaxiAdvancesAlongWaypoints(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatalf("taxi: %s", r.Message)
	}

	e.mu.Lock()
	sim := e.aircraft[cs]
	if len(sim.TaxiWaypoints) < 2 {
		e.mu.Unlock()
		t.Fatalf("need path, got %d wps", len(sim.TaxiWaypoints))
	}
	startLat, startLon := sim.Lat, sim.Lon
	first := sim.TaxiWaypoints[0]
	pathLen := 0.0
	prev := Point{Lat: startLat, Lon: startLon}
	for _, wp := range sim.TaxiWaypoints {
		pathLen += geo.Distance(prev.Lat, prev.Lon, wp.Lat, wp.Lon)
		prev = wp
	}
	e.mu.Unlock()

	e.Unpause()
	// Several ticks should close distance toward first waypoint / along path.
	var lastDist float64 = math.MaxFloat64
	progressed := false
	for i := 0; i < 30; i++ {
		tr := e.Tick(time.Second)
		if len(tr.Updates) == 0 && i > 0 {
			// May stop at hold-short of dep runway before 30s if path short.
			break
		}
		ac, _ := e.Get(cs)
		d0 := geo.Distance(ac.Lat, ac.Lon, first.Lat, first.Lon)
		// Distance from start should increase as we taxi (or we reach hold).
		fromStart := geo.Distance(startLat, startLon, ac.Lat, ac.Lon)
		if fromStart > 5 {
			progressed = true
		}
		if ac.Status == StatusHoldingShort {
			if ac.HoldShortOf != "33" {
				t.Errorf("hold short of %q, want 33", ac.HoldShortOf)
			}
			if ac.Speed != 0 {
				t.Errorf("speed at hold = %v", ac.Speed)
			}
			progressed = true
			break
		}
		_ = d0
		_ = lastDist
		lastDist = fromStart
	}
	if !progressed {
		t.Fatalf("taxi did not progress; pathLen=%.0fm start=(%v,%v)", pathLen, startLat, startLon)
	}
}

func TestTick_TaxiStopsAtHoldShort(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	placeOnSurface(t, e, cs, "A")

	r = e.Command(cs, "taxi B 19 hs 19")
	if !r.OK {
		t.Fatalf("taxi: %s", r.Message)
	}

	e.Unpause()
	// Drive long enough to reach hold (KBTV A-B path is short).
	held := false
	for i := 0; i < 120; i++ {
		e.Tick(time.Second)
		ac, _ := e.Get(cs)
		if ac.Status == StatusHoldingShort {
			held = true
			if ac.HoldShortOf != "19" {
				t.Errorf("HoldShortOf = %q", ac.HoldShortOf)
			}
			if ac.Speed != 0 {
				t.Errorf("speed = %v at hold", ac.Speed)
			}
			// Further ticks while holding short must not move.
			lat, lon := ac.Lat, ac.Lon
			e.Tick(5 * time.Second)
			ac2, _ := e.Get(cs)
			if ac2.Lat != lat || ac2.Lon != lon || ac2.Status != StatusHoldingShort {
				t.Error("must freeze while holding short")
			}
			break
		}
	}
	if !held {
		ac, _ := e.Get(cs)
		t.Fatalf("never held short; status=%s hold=%s wp=%d", ac.Status, ac.HoldShortOf, ac.TaxiWPIndex)
	}

	// cross clears the hold; destination runway path may finish immediately
	// into Holding Short of DepRunway (same rwy) when the hold was the last WP.
	r = e.Command(cs, "cross 19")
	if !r.OK {
		t.Fatalf("cross: %s", r.Message)
	}
	afterCross := e.mustGet(t, cs)
	if afterCross.Status != StatusTaxiing && afterCross.Status != StatusHoldingShort {
		t.Errorf("after cross status = %s", afterCross.Status)
	}
	// Intermediate-hold case: present-position hold freezes, res resumes motion.
	// (Destination hs often ends the path; cover resume via hold/res instead.)
	placeOnSurface(t, e, cs, "A")
	r = e.Command(cs, "taxi B 19")
	if !r.OK {
		t.Fatalf("re-taxi: %s", r.Message)
	}
	e.Tick(time.Second)
	if !e.Command(cs, "hold").OK {
		t.Fatal("hold")
	}
	lat, lon := e.mustGet(t, cs).Lat, e.mustGet(t, cs).Lon
	e.Tick(3 * time.Second)
	if e.mustGet(t, cs).Lat != lat {
		t.Error("hold froze?")
	}
	if !e.Command(cs, "res").OK {
		t.Fatal("res")
	}
	moved := false
	for i := 0; i < 20; i++ {
		e.Tick(time.Second)
		after := e.mustGet(t, cs)
		if geo.Distance(lat, lon, after.Lat, after.Lon) > 2 {
			moved = true
			break
		}
	}
	if !moved {
		t.Error("expected motion after res")
	}
}

func TestTick_HoldFreezesThenRes(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "taxi J 33")
	if !r.OK {
		t.Fatal(r.Message)
	}
	e.Unpause()
	// Move a bit.
	e.Tick(2 * time.Second)
	r = e.Command(cs, "hold")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusHolding {
		t.Fatalf("status = %s", ac.Status)
	}
	lat, lon := ac.Lat, ac.Lon
	e.Tick(5 * time.Second)
	ac2 := e.mustGet(t, cs)
	if ac2.Lat != lat || ac2.Lon != lon {
		t.Error("hold must freeze position")
	}

	r = e.Command(cs, "res")
	if !r.OK {
		t.Fatal(r.Message)
	}
	// Resume taxi.
	moved := false
	for i := 0; i < 10; i++ {
		e.Tick(time.Second)
		ac3 := e.mustGet(t, cs)
		if geo.Distance(lat, lon, ac3.Lat, ac3.Lon) > 2 {
			moved = true
			break
		}
	}
	if !moved {
		t.Error("res should allow taxi progress")
	}
}

// --- Takeoff ----------------------------------------------------------------

func TestTick_TakeoffAcceleratesAndClimbs(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	// Put on runway 33 threshold and clear takeoff.
	e.mu.Lock()
	ac := e.aircraft[cs]
	s := e.airport.FindSurface("33")
	if s == nil {
		e.mu.Unlock()
		t.Fatal("no rwy 33")
	}
	thr, hdg, ok := runwayThreshold(s, "33")
	if !ok {
		e.mu.Unlock()
		t.Fatal("threshold")
	}
	ac.Lat, ac.Lon = thr.Lat, thr.Lon
	ac.Heading = hdg
	ac.Alt = e.airport.FieldElev
	ac.Speed = 0
	ac.Status = StatusHoldingInPosition
	ac.PositionHold = true
	ac.DepRunway = "33"
	ac.CurrentSurface = "33"
	ac.TaxiWaypoints = nil
	e.mu.Unlock()

	r = e.Command(cs, "cto 330")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).Status != StatusTakeoff {
		t.Fatalf("status after cto = %s", e.mustGet(t, cs).Status)
	}

	e.Unpause()
	field := e.Airport().FieldElev
	target := initialClimbTarget(e.Airport(), EnginePiston)
	// Accelerate on roll then climb. Use 2s steps; 2500 fpm needs ~4 min to 10k.
	var sawSpeed, sawAirborne, sawDeparting bool
	var maxAlt float64
	for i := 0; i < 200; i++ {
		e.Tick(2 * time.Second)
		ac := e.mustGet(t, cs)
		if ac.Speed > 10 {
			sawSpeed = true
		}
		if ac.Alt > field+10 {
			sawAirborne = true
		}
		if ac.Alt > maxAlt {
			maxAlt = ac.Alt
		}
		if ac.Status == StatusDeparting {
			sawDeparting = true
			break
		}
	}
	if !sawSpeed {
		t.Error("expected takeoff acceleration")
	}
	if !sawAirborne {
		t.Error("expected climb above field")
	}
	if !sawDeparting {
		t.Errorf("expected StatusDeparting; maxAlt=%.0f target=%.0f status=%s",
			maxAlt, target, e.mustGet(t, cs).Status)
	}
	final := e.mustGet(t, cs)
	if math.Abs(final.Alt-target) > 50 {
		t.Errorf("alt = %.0f, want ~%.0f", final.Alt, target)
	}
}

// --- Air vectors ------------------------------------------------------------

func TestTick_VectorTurnClimbSpeed(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Force known state.
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Heading = 0
	ac.Alt = 5000
	ac.Speed = 180
	ac.Status = StatusAirborne
	e.mu.Unlock()

	e.Command(cs, "fh 090")
	e.Command(cs, "cm 8000")
	e.Command(cs, "spd 220")
	e.Unpause()

	// 30s of motion: turn rate 3°/s → 90° in 30s; climb 1500 fpm → +750 ft;
	// speed +8 kt/s → would overshoot but caps at 220.
	for i := 0; i < 30; i++ {
		tr := e.Tick(time.Second)
		if len(tr.Updates) != 1 {
			t.Fatalf("tick %d: updates=%d", i, len(tr.Updates))
		}
		// Copies only — mutating result must not affect engine.
		tr.Updates[0].Lat = 0
		tr.Updates[0].Heading = 999
	}
	acSnap := e.mustGet(t, cs)
	// Heading should be near 090.
	if math.Abs(headingDelta(acSnap.Heading, 90)) > 2 {
		t.Errorf("heading = %v, want ~90", acSnap.Heading)
	}
	// Alt climbed.
	if acSnap.Alt < 5500 {
		t.Errorf("alt = %v, expected climb toward 8000", acSnap.Alt)
	}
	// Speed increased.
	if acSnap.Speed < 200 {
		t.Errorf("speed = %v, expected increase toward 220", acSnap.Speed)
	}
	// Position moved east-ish (heading ~90).
	// (lon should increase in northern hemisphere roughly)
	if acSnap.Lon == 0 {
		t.Error("position not integrated")
	}
}

func TestTick_TurnDirRespectsLeftRight(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	// From heading 0, turn right to 270 → long way (270° right).
	e.mu.Lock()
	e.aircraft[cs].Heading = 0
	e.aircraft[cs].Speed = 150
	e.aircraft[cs].Status = StatusAirborne
	e.mu.Unlock()

	e.Command(cs, "tr 270")
	e.Unpause()
	// After 10s at 3°/s right → heading 30.
	for i := 0; i < 10; i++ {
		e.Tick(time.Second)
	}
	h := e.mustGet(t, cs).Heading
	if h < 20 || h > 40 {
		t.Errorf("tr from 0 toward 270: after 10s hdg=%v, want ~30", h)
	}

	// Left turn from 0 to 90 → long way left (270° left) → after 10s heading 330.
	e.mu.Lock()
	e.aircraft[cs].Heading = 0
	e.aircraft[cs].ImmediateHeading = false
	e.mu.Unlock()
	e.Command(cs, "tl 090")
	for i := 0; i < 10; i++ {
		e.Tick(time.Second)
	}
	h = e.mustGet(t, cs).Heading
	// 0 - 30 = 330
	if h < 320 || h > 340 {
		t.Errorf("tl from 0 toward 90: after 10s hdg=%v, want ~330", h)
	}
}

func TestTick_FlyHeadingNowClearsImmediate(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "fhn 180")
	if !e.mustGet(t, cs).ImmediateHeading {
		t.Fatal("setup: ImmediateHeading")
	}
	e.Unpause()
	e.Tick(time.Second)
	ac := e.mustGet(t, cs)
	if ac.ImmediateHeading {
		t.Error("tick should clear ImmediateHeading")
	}
	if math.Abs(ac.Heading-180) > 1 {
		t.Errorf("heading = %v", ac.Heading)
	}
}

func TestTick_ShortestTurn(t *testing.T) {
	// From 10° to 350° shortest is left 20°, not right 340°.
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	e.aircraft[cs].Heading = 10
	e.aircraft[cs].Speed = 100
	e.aircraft[cs].Status = StatusAirborne
	e.mu.Unlock()
	e.Command(cs, "fh 350")
	e.Unpause()
	e.Tick(time.Second) // 3° left → ~7
	h := e.mustGet(t, cs).Heading
	// Should decrease toward 0/350.
	if h > 10 {
		t.Errorf("shortest turn should go left from 10 toward 350; hdg=%v", h)
	}
}

func TestTick_UpdatesAreCopies(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "fh 045")
	e.Unpause()
	tr := e.Tick(time.Second)
	if len(tr.Updates) != 1 {
		t.Fatalf("updates = %d", len(tr.Updates))
	}
	// Mutate returned snapshot.
	tr.Updates[0].Callsign = "HACKED"
	tr.Updates[0].Lat = 99
	got, ok := e.Get(cs)
	if !ok || got.Callsign != cs || got.Lat == 99 {
		t.Error("TickResult must be value copies, not shared state")
	}
}

func TestTick_OnlyChangedAircraftInUpdates(t *testing.T) {
	e := loadKBTVEngine(t)
	// Parked aircraft + airborne.
	r := e.CommandLine("add v s p @GA1")
	if !r.OK {
		t.Fatal(r.Message)
	}
	parked := r.Added[0].Callsign
	air := addAirborne(t, e)
	e.Command(air, "fh 100")
	e.Unpause()
	tr := e.Tick(time.Second)
	for _, u := range tr.Updates {
		if u.Callsign == parked {
			t.Error("parked aircraft should not appear in updates when frozen")
		}
	}
	found := false
	for _, u := range tr.Updates {
		if u.Callsign == air {
			found = true
		}
	}
	if !found {
		t.Error("airborne aircraft with vectors should be in updates")
	}
}

func TestHeadingDelta(t *testing.T) {
	tests := []struct {
		cur, tgt, want float64
	}{
		{0, 90, 90},
		{0, 270, -90},
		{10, 350, -20},
		{350, 10, 20},
		{180, 180, 0},
	}
	for _, tc := range tests {
		got := headingDelta(tc.cur, tc.tgt)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("headingDelta(%v,%v)=%v want %v", tc.cur, tc.tgt, got, tc.want)
		}
	}
}

func TestTurnToward_Helpers(t *testing.T) {
	ac := &SimAircraft{Heading: 0}
	if !turnToward(ac, 90, TurnShortest, 10) { // 30° step
		t.Fatal("expected change")
	}
	if math.Abs(ac.Heading-30) > 0.01 {
		t.Errorf("hdg=%v", ac.Heading)
	}
	// Reach target.
	ac.Heading = 89
	if !turnToward(ac, 90, TurnShortest, 1) {
		t.Fatal("expected snap")
	}
	if ac.Heading != 90 {
		t.Errorf("hdg=%v", ac.Heading)
	}
	// Already there.
	if turnToward(ac, 90, TurnShortest, 1) {
		t.Error("no change expected")
	}
}

func TestClimbAndSpeedToward(t *testing.T) {
	ac := &SimAircraft{Alt: 1000, Speed: 100}
	// 1500 fpm * 10s = 250 ft → 1250
	if !climbToward(ac, 5000, 10) {
		t.Fatal("climb")
	}
	if math.Abs(ac.Alt-1250) > 0.1 {
		t.Errorf("alt=%v want 1250", ac.Alt)
	}
	// Large step snaps to target.
	ac.Alt = 1900
	climbToward(ac, 2000, 60)
	if ac.Alt != 2000 {
		t.Errorf("snap alt=%v", ac.Alt)
	}
	ac.Alt = 1998
	climbToward(ac, 2000, 1)
	if ac.Alt != 2000 {
		t.Errorf("near snap alt=%v", ac.Alt)
	}

	ac.Speed = 100
	speedToward(ac, 150, 1) // +8
	if math.Abs(ac.Speed-108) > 0.01 {
		t.Errorf("spd=%v", ac.Speed)
	}
	ac.Speed = 149
	speedToward(ac, 150, 1)
	if ac.Speed != 150 {
		t.Errorf("snap spd=%v", ac.Speed)
	}
}

func TestTick_TaxiToParking(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	// Short path to nearby parking if possible; otherwise force path completion.
	r = e.Command(cs, "taxi @GA9")
	// May fail "Already there!" — move via J then back.
	if !r.OK {
		r = e.Command(cs, "taxi J @GA1")
		if !r.OK {
			t.Fatalf("taxi to parking: %s", r.Message)
		}
	}
	e.Unpause()
	parked := false
	for i := 0; i < 300; i++ {
		tr := e.Tick(2 * time.Second)
		ac, ok := e.Get(cs)
		if !ok {
			// Deleted?
			for _, d := range tr.Deletes {
				if d == cs {
					parked = true
				}
			}
			break
		}
		if ac.Status == StatusParked {
			parked = true
			if ac.Parking == "" {
				t.Error("Parking empty when parked")
			}
			break
		}
	}
	if !parked {
		ac, _ := e.Get(cs)
		t.Fatalf("never parked; status=%s park=%s", ac.Status, ac.Parking)
	}
}

func TestTick_DeleteArrivalsWhenParked(t *testing.T) {
	e := NewEngineSettings(Settings{DeleteArrivalsWhenParked: true})
	data := kbtvFixture(t, "KBTV_example.apt")
	apt, errs := ParseAPT(string(data))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if err := e.LoadAirport(&apt); err != nil {
		t.Fatal(err)
	}
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	// Force finish path to parking via engine state.
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusTaxiing
	ac.TaxiParking = "GA1"
	ac.TaxiWaypoints = []Point{{Lat: ac.Lat, Lon: ac.Lon}}
	ac.TaxiWPIndex = 0
	ac.TaxiHolds = nil
	e.mu.Unlock()
	e.Unpause()
	tr := e.Tick(time.Second)
	if len(tr.Deletes) != 1 || tr.Deletes[0] != cs {
		t.Fatalf("deletes = %v", tr.Deletes)
	}
	if _, ok := e.Get(cs); ok {
		t.Error("aircraft should be deleted")
	}
}

func TestTick_TaxiEndClearedTakeoff(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusTaxiing
	ac.ClearedTakeoff = true
	ac.DepRunway = "33"
	ac.TaxiWaypoints = []Point{{Lat: ac.Lat, Lon: ac.Lon}}
	ac.TaxiWPIndex = 0
	ac.TaxiParking = ""
	ac.TaxiHolds = nil
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second)
	ac2 := e.mustGet(t, cs)
	if ac2.Status != StatusTakeoff {
		t.Errorf("status = %s, want Takeoff", ac2.Status)
	}
}

func TestTick_TaxiEndTaxiwayOnly(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusTaxiing
	ac.DepRunway = ""
	ac.TaxiParking = ""
	ac.TaxiWaypoints = []Point{{Lat: ac.Lat, Lon: ac.Lon}}
	ac.TaxiWPIndex = 0
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second)
	if e.mustGet(t, cs).Status != StatusTaxiing {
		t.Errorf("status = %s", e.mustGet(t, cs).Status)
	}
}

func TestTick_HoldingShortOfDepWithCTO(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusHoldingShort
	ac.HoldShortOf = "33"
	ac.DepRunway = "33"
	ac.ClearedTakeoff = true
	ac.Speed = 5
	ac.Alt = e.airport.FieldElev
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second)
	ac2 := e.mustGet(t, cs)
	if ac2.Status != StatusTakeoff {
		t.Errorf("status = %s, want Takeoff", ac2.Status)
	}
}

func TestTick_LandedDecel(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add i l j 33 1")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusLanded
	ac.Speed = 40
	ac.Heading = 90
	ac.TaxiWaypoints = nil
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second)
	ac2 := e.mustGet(t, cs)
	if ac2.Speed >= 40 {
		t.Errorf("speed should decrease: %v", ac2.Speed)
	}
	// Continue until stop.
	for i := 0; i < 20; i++ {
		e.Tick(time.Second)
	}
	if e.mustGet(t, cs).Speed != 0 {
		t.Errorf("speed = %v want 0", e.mustGet(t, cs).Speed)
	}
}

func TestTick_NoTaxiPathZeroSpeed(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusTaxiing
	ac.Speed = 15
	ac.TaxiWaypoints = nil
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second)
	if e.mustGet(t, cs).Speed != 0 {
		t.Errorf("speed = %v", e.mustGet(t, cs).Speed)
	}
}

func TestRotateAndClimbHelpers(t *testing.T) {
	for _, eng := range []string{EnginePiston, EngineTurboprop, EngineJet, EngineHelicopter, "X"} {
		if rotateSpeedKt(eng) <= 0 {
			t.Errorf("rotate %s", eng)
		}
	}
	if initialClimbTarget(nil, EngineJet) != 3000 {
		t.Error("nil apt")
	}
	apt := &Airport{FieldElev: 500, InitClimbProps: 3000, InitClimbJets: 5000}
	if initialClimbTarget(apt, EngineJet) != 5000 {
		t.Error("jet climb")
	}
	if initialClimbTarget(apt, EnginePiston) != 3000 {
		t.Error("prop climb")
	}
	// AGL-style when below field.
	apt2 := &Airport{FieldElev: 1000, InitClimbProps: 500, InitClimbJets: 0}
	if initialClimbTarget(apt2, EnginePiston) != 1500 {
		t.Errorf("agl = %v", initialClimbTarget(apt2, EnginePiston))
	}
	if initialClimbTarget(apt2, EngineJet) != 3000 {
		t.Errorf("jet zero fallback = %v", initialClimbTarget(apt2, EngineJet))
	}
	if fieldElevFeet(nil) != 0 {
		t.Error("nil field")
	}
	if fieldElevFeet(apt) != 500 {
		t.Error("field")
	}
}

func TestSpeedTowardDescendAndDecel(t *testing.T) {
	ac := &SimAircraft{Alt: 5000, Speed: 200}
	climbToward(ac, 4000, 10) // descend 250 ft
	if ac.Alt >= 5000 {
		t.Errorf("alt=%v", ac.Alt)
	}
	speedToward(ac, 100, 1)
	if ac.Speed >= 200 {
		t.Errorf("spd=%v", ac.Speed)
	}
	speedToward(ac, -5, 100)
	if ac.Speed != 0 {
		t.Errorf("neg target → %v", ac.Speed)
	}
	// Already there.
	ac.Speed = 150
	if speedToward(ac, 150, 1) {
		t.Error("no change")
	}
	if climbToward(ac, ac.Alt, 1) {
		t.Error("alt no change")
	}
}

func TestTurnTowardForcedDirs(t *testing.T) {
	ac := &SimAircraft{Heading: 0}
	// Right all the way: small step.
	turnToward(ac, 350, TurnRight, 1) // +3
	if ac.Heading < 2 {
		t.Errorf("right hdg=%v", ac.Heading)
	}
	ac.Heading = 0
	turnToward(ac, 10, TurnLeft, 1) // -3 → 357
	if ac.Heading > 358 && ac.Heading < 2 {
		// around 357
	} else if ac.Heading < 350 {
		t.Errorf("left hdg=%v", ac.Heading)
	}
	// Complete forced turn in one large step.
	ac.Heading = 0
	turnToward(ac, 10, TurnRight, 10) // 30° step > 10 need
	if ac.Heading != 10 {
		t.Errorf("snap right = %v", ac.Heading)
	}
	ac.Heading = 0
	turnToward(ac, 350, TurnLeft, 10)
	if math.Abs(ac.Heading-350) > 0.01 {
		t.Errorf("snap left = %v", ac.Heading)
	}
}

func TestTick_EmptyDtAndNilAircraft(t *testing.T) {
	e := NewEngine()
	if e.Tick(0).Updates != nil && len(e.Tick(0).Updates) != 0 {
		t.Error("dt=0")
	}
	// Direct helper coverage.
	if changed, del := e.tickAircraftLocked(nil, 1); changed || del {
		t.Error("nil ac")
	}
	e.mu.Lock()
	res := e.expandTickLocked(0)
	e.mu.Unlock()
	if len(res.Updates) != 0 {
		t.Error("dtSec=0 expand")
	}
}

// Issue 1 regression: multi-WP path with hold at last index must follow the
// polyline (east then north) — no corner-cut straight to the hold.
func TestTick_TaxiFollowsPolylineUntilHold(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign

	// Synthetic L-shaped path: east along constant lat, then north to hold.
	// ~0.01° lon ≈ 800–900 m east; ~0.01° lat ≈ 1100 m north.
	const lat0 = 44.4700
	const lon0 = -73.1500
	const lonMid = -73.1400 // end of east leg
	const latHold = 44.4800 // hold at north end
	eastEnd := Point{Lat: lat0, Lon: lonMid}
	holdPt := Point{Lat: latHold, Lon: lonMid}

	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Lat, ac.Lon = lat0, lon0
	ac.Heading = 90
	ac.Status = StatusTaxiing
	ac.TaxiWaypoints = []Point{
		{Lat: lat0, Lon: lon0 + 0.003},   // WP0 mid-east
		eastEnd,                          // WP1 end of east leg
		{Lat: lat0 + 0.005, Lon: lonMid}, // WP2 mid-north
		holdPt,                           // WP3 hold
	}
	ac.TaxiWPIndex = 0
	ac.TaxiHolds = []TaxiHold{{
		Name:          "H1",
		Point:         holdPt,
		WaypointIndex: 3,
	}}
	ac.TaxiParking = ""
	ac.DepRunway = ""
	e.mu.Unlock()

	e.Unpause()

	// While still on the east leg (lon not yet past mid-point of east segment),
	// latitude must not jump north toward the hold.
	eastMidLon := (lon0 + lonMid) / 2
	for i := 0; i < 400; i++ {
		e.Tick(time.Second)
		snap := e.mustGet(t, cs)
		if snap.Status == StatusHoldingShort {
			break
		}
		// Early east-leg check: until lon past east mid, lat must stay near lat0.
		if snap.Lon < eastMidLon {
			if snap.Lat > lat0+0.001 { // ~100 m north is already a corner-cut
				t.Fatalf("corner-cut on east leg: lat=%.6f lon=%.6f (wpIdx path should stay ~lat0 until lon mid)",
					snap.Lat, snap.Lon)
			}
		}
	}

	final := e.mustGet(t, cs)
	if final.Status != StatusHoldingShort {
		t.Fatalf("expected hold short, got %s lat=%.5f lon=%.5f", final.Status, final.Lat, final.Lon)
	}
	if final.HoldShortOf != "H1" {
		t.Errorf("HoldShortOf = %q", final.HoldShortOf)
	}
	// Must be near hold point, not still on east leg start.
	dHold := geo.Distance(final.Lat, final.Lon, holdPt.Lat, holdPt.Lon)
	if dHold > 20 {
		t.Errorf("stopped %.0fm from hold (lat=%.5f lon=%.5f)", dHold, final.Lat, final.Lon)
	}
	// And we must have actually traveled east first: lon should be near lonMid.
	if final.Lon < lonMid-0.002 {
		t.Errorf("final lon=%.5f, expected near %.5f (followed east then north)", final.Lon, lonMid)
	}
}

// Issue 2 regression: cto from hold-short with wrong heading aligns to runway
// and rolls along centerline, not the old taxi heading.
func TestTick_CTOAlignsRunwayHeadingFromHoldShort(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign

	s := e.Airport().FindSurface("33")
	if s == nil {
		t.Fatal("no rwy 33")
	}
	thr, rwyHdg, ok := runwayThreshold(s, "33")
	if !ok {
		t.Fatal("threshold")
	}
	// Perpendicular to runway (taxi-entry style).
	wrongHdg := normalizeHeading(rwyHdg + 90)

	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Lat, ac.Lon = thr.Lat, thr.Lon
	ac.Heading = wrongHdg
	ac.Alt = e.airport.FieldElev
	ac.Speed = 0
	ac.Status = StatusHoldingShort
	ac.HoldShortOf = "33"
	ac.DepRunway = "33"
	ac.CurrentSurface = "33"
	ac.TaxiWaypoints = nil
	ac.ClearedTakeoff = false
	e.mu.Unlock()

	// cto from hold-short of dep runway → StatusTakeoff + align heading.
	r = e.Command(cs, "cto")
	if !r.OK {
		t.Fatal(r.Message)
	}
	afterCTO := e.mustGet(t, cs)
	if afterCTO.Status != StatusTakeoff {
		t.Fatalf("status after cto = %s", afterCTO.Status)
	}
	if math.Abs(headingDelta(afterCTO.Heading, rwyHdg)) > 1 {
		t.Errorf("heading after cto = %.1f, want runway %.1f (was wrong %.1f)",
			afterCTO.Heading, rwyHdg, wrongHdg)
	}

	// Roll a few seconds; position should advance along runway heading, not +90°.
	startLat, startLon := afterCTO.Lat, afterCTO.Lon
	e.Unpause()
	for i := 0; i < 5; i++ {
		e.Tick(time.Second)
	}
	rolled := e.mustGet(t, cs)
	if rolled.Speed < 20 {
		t.Fatalf("expected roll speed, got %.1f", rolled.Speed)
	}
	// Bearing from start to current should be near runway heading.
	movedBrg := initialBearingDeg(startLat, startLon, rolled.Lat, rolled.Lon)
	if math.Abs(headingDelta(movedBrg, rwyHdg)) > 15 {
		t.Errorf("roll track heading %.1f, want ~runway %.1f (not taxi %.1f)",
			movedBrg, rwyHdg, wrongHdg)
	}
	// Must not still be on the old perpendicular heading.
	if math.Abs(headingDelta(rolled.Heading, wrongHdg)) < 5 {
		t.Errorf("still on wrong heading %.1f", rolled.Heading)
	}
}

// Issue 2: tick transition path (ClearedTakeoff already set, HoldingInPosition).
func TestTick_CTOAlignsFromInPositionOnTick(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign

	s := e.Airport().FindSurface("33")
	thr, rwyHdg, ok := runwayThreshold(s, "33")
	if !ok {
		t.Fatal("threshold")
	}
	wrongHdg := normalizeHeading(rwyHdg + 90)

	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Lat, ac.Lon = thr.Lat, thr.Lon
	ac.Heading = wrongHdg
	ac.Alt = e.airport.FieldElev
	ac.Speed = 0
	ac.Status = StatusHoldingInPosition
	ac.PositionHold = true
	ac.DepRunway = "33"
	ac.ClearedTakeoff = true // set without going through atRunway cmd branch
	ac.TaxiWaypoints = nil
	e.mu.Unlock()

	e.Unpause()
	e.Tick(time.Second)
	snap := e.mustGet(t, cs)
	if snap.Status != StatusTakeoff {
		t.Fatalf("status = %s", snap.Status)
	}
	if math.Abs(headingDelta(snap.Heading, rwyHdg)) > 1 {
		t.Errorf("tick transition heading = %.1f, want runway %.1f", snap.Heading, rwyHdg)
	}
}
