package sweatbox

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

// --- Geometry ---------------------------------------------------------------

func TestComputePatternAnchors_LeftRight(t *testing.T) {
	e := loadKBTVEngine(t)
	s := e.Airport().FindSurface("33")
	if s == nil {
		t.Fatal("runway 33")
	}
	left, err := computePatternAnchors(s, "33", "L", 1.0)
	if err != "" {
		t.Fatal(err)
	}
	right, err := computePatternAnchors(s, "33", "R", 1.0)
	if err != "" {
		t.Fatal(err)
	}
	if left.Traffic != "L" || right.Traffic != "R" {
		t.Fatalf("traffic L=%s R=%s", left.Traffic, right.Traffic)
	}
	// Lateral headings should be opposite.
	if math.Abs(headingDelta(left.LatHdg, right.LatHdg)) < 170 {
		t.Errorf("lat headings should be ~180 apart: L=%.1f R=%.1f", left.LatHdg, right.LatHdg)
	}
	// Downwind offset ≈ 1 NM from centerline mid.
	mid := Point{Lat: (left.Threshold.Lat + left.FarEnd.Lat) / 2, Lon: (left.Threshold.Lon + left.FarEnd.Lon) / 2}
	dL := geo.Distance(mid.Lat, mid.Lon, left.MidfieldDW.Lat, left.MidfieldDW.Lon)
	dR := geo.Distance(mid.Lat, mid.Lon, right.MidfieldDW.Lat, right.MidfieldDW.Lon)
	if math.Abs(dL-metersPerNM) > 80 {
		t.Errorf("left midfield offset = %.0fm, want ~%.0f", dL, metersPerNM)
	}
	if math.Abs(dR-metersPerNM) > 80 {
		t.Errorf("right midfield offset = %.0fm, want ~%.0f", dR, metersPerNM)
	}
	// Left and right midfields should be different sides.
	if geo.Distance(left.MidfieldDW.Lat, left.MidfieldDW.Lon, right.MidfieldDW.Lat, right.MidfieldDW.Lon) < 1000 {
		t.Error("left/right midfield too close")
	}
	// Final start ~1 NM from threshold opposite landing.
	df := geo.Distance(left.Threshold.Lat, left.Threshold.Lon, left.FinalStart.Lat, left.FinalStart.Lon)
	if math.Abs(df-metersPerNM) > 80 {
		t.Errorf("final length = %.0fm, want ~%.0f", df, metersPerNM)
	}
	// Leg headings: final == landing, downwind == landing+180.
	if math.Abs(headingDelta(left.legHeading(StatusFinal), left.LandingHdg)) > 0.1 {
		t.Error("final heading")
	}
	if math.Abs(headingDelta(left.legHeading(StatusDownwind), left.LandingHdg+180)) > 0.1 {
		t.Error("downwind heading")
	}
}

func TestComputePatternAnchors_InvalidRunway(t *testing.T) {
	_, err := computePatternAnchors(nil, "33", "L", 1)
	if err == "" {
		t.Error("expected error for nil surface")
	}
	e := loadKBTVEngine(t)
	s := e.Airport().FindSurface("GA1")
	// parking surface
	if s != nil {
		_, err = computePatternAnchors(s, "GA1", "L", 1)
		if err == "" {
			t.Error("expected error for non-runway")
		}
	}
}

func TestNextPrevLeg(t *testing.T) {
	order := []string{StatusUpwind, StatusCrosswind, StatusDownwind, StatusBase, StatusFinal}
	for i, leg := range order {
		n := nextLeg(leg)
		want := order[(i+1)%len(order)]
		if n != want {
			t.Errorf("next(%s)=%s want %s", leg, n, want)
		}
		if prevLeg(n) != leg {
			t.Errorf("prev(next(%s)) != %s", leg, leg)
		}
	}
}

// --- Enter pattern commands -------------------------------------------------

func TestCommand_EnterPattern_Downwind(t *testing.T) {
	e := loadKBTVEngine(t)
	// Add airborne then eld.
	cs := addAirborne(t, e)
	r := e.Command(cs, "eld 33")
	if !r.OK {
		t.Fatalf("eld: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusDownwind {
		t.Fatalf("status = %s", ac.Status)
	}
	if ac.PatternTraffic != "L" {
		t.Errorf("traffic = %s", ac.PatternTraffic)
	}
	if ac.LandingRunway != "33" {
		t.Errorf("LandingRunway = %s", ac.LandingRunway)
	}
	if !ac.InPattern {
		t.Error("InPattern")
	}
	if ac.LandingType != LandingTG {
		t.Errorf("default landing type = %s", ac.LandingType)
	}
	if !strings.Contains(ac.Instruction, "Downwind") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
	// Altitude near pattern elev.
	if math.Abs(ac.Alt-1335) > 50 {
		t.Errorf("alt = %.0f, want ~1335", ac.Alt)
	}
}

func TestCommand_EnterPattern_RightCrosswindAndFinal(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	r := e.Command(cs, "erc 15")
	if !r.OK {
		t.Fatalf("erc: %s", r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusCrosswind || ac.PatternTraffic != "R" {
		t.Fatalf("status=%s traffic=%s", ac.Status, ac.PatternTraffic)
	}

	r = e.Command(cs, "ef 15")
	if !r.OK {
		t.Fatalf("ef: %s", r.Message)
	}
	ac = e.mustGet(t, cs)
	if ac.Status != StatusFinal {
		t.Fatalf("status = %s", ac.Status)
	}
	// ef keeps prior right traffic.
	if ac.PatternTraffic != "R" {
		t.Errorf("traffic after ef = %s", ac.PatternTraffic)
	}
}

func TestCommand_EnterPattern_Errors(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	r := e.Command(cs, "eld")
	if r.OK {
		t.Error("eld without runway should fail")
	}
	r = e.Command(cs, "eld 99")
	if r.OK {
		t.Error("unknown runway should fail")
	}
	r = e.Command("", "eld 33")
	if r.OK {
		t.Error("no aircraft selected")
	}
	e2 := NewEngine()
	// no airport
	e2.mu.Lock()
	e2.aircraft["X"] = &SimAircraft{Callsign: "X", Status: StatusAirborne}
	e2.mu.Unlock()
	r = e2.Command("X", "eld 33")
	if r.OK {
		t.Error("no airport should fail")
	}
}

// --- Landing types / ga / ext / ps / mlt ------------------------------------

func TestCommand_LandingTypes(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33")

	for _, tc := range []struct {
		cmd  string
		want string
	}{
		{"tg", LandingTG},
		{"la", LandingLA},
		{"fs", LandingFS},
		{"sg 20", LandingSG},
	} {
		r := e.Command(cs, tc.cmd)
		if !r.OK {
			t.Fatalf("%s: %s", tc.cmd, r.Message)
		}
		ac := e.mustGet(t, cs)
		if ac.LandingType != tc.want {
			t.Errorf("%s: LandingType=%s want %s", tc.cmd, ac.LandingType, tc.want)
		}
	}
	ac := e.mustGet(t, cs)
	if ac.SGWaitSec != 20 {
		t.Errorf("SGWaitSec = %v", ac.SGWaitSec)
	}
}

func TestCommand_LandingType_OnApproach(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p 33 5")
	if !r.OK {
		t.Fatal(r.Message)
	}
	cs := r.Added[0].Callsign
	if e.mustGet(t, cs).Status != StatusOnApproach {
		t.Fatalf("status = %s", e.mustGet(t, cs).Status)
	}
	r = e.Command(cs, "fs")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.LandingType != LandingFS {
		t.Errorf("type = %s", ac.LandingType)
	}
	if ac.Status != StatusFinal {
		t.Errorf("status = %s, want Final", ac.Status)
	}
}

func TestCommand_ExtendAndTurn(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33") // Downwind

	r := e.Command(cs, "ext")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if !e.mustGet(t, cs).ExtendLeg {
		t.Error("ExtendLeg")
	}
	if !strings.Contains(e.mustGet(t, cs).Instruction, "extending") {
		t.Errorf("instruction = %q", e.mustGet(t, cs).Instruction)
	}

	// tb from downwind.
	r = e.Command(cs, "tb")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusBase {
		t.Fatalf("status = %s", ac.Status)
	}
	if ac.ExtendLeg {
		t.Error("ExtendLeg should clear on turn")
	}

	// tc while on base should fail.
	r = e.Command(cs, "tc")
	if r.OK {
		t.Error("tc from base should fail")
	}

	// ext on base fails.
	r = e.Command(cs, "ext")
	if r.OK {
		t.Error("ext on base should fail")
	}
}

func TestCommand_GoAround(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "ef 33")
	e.Command(cs, "fs")

	r := e.Command(cs, "ga")
	if !r.OK {
		t.Fatal(r.Message)
	}
	ac := e.mustGet(t, cs)
	if ac.Status != StatusUpwind {
		t.Fatalf("status = %s", ac.Status)
	}
	if !ac.InPattern {
		t.Error("InPattern")
	}
	if !strings.Contains(strings.ToLower(ac.Instruction), "go around") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
}

func TestCommand_PatternSize(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)

	r := e.Command(cs, "ps 2")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).PatternSizeNM != 2 {
		t.Errorf("size = %v", e.mustGet(t, cs).PatternSizeNM)
	}
	r = e.Command(cs, "ps 0.1")
	if r.OK {
		t.Error("size too small should fail")
	}
	r = e.Command(cs, "ps 50")
	if r.OK {
		t.Error("size too large should fail")
	}
	r = e.Command(cs, "ps")
	if r.OK {
		t.Error("ps without arg should fail")
	}
}

func TestCommand_MLT_MRT(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign

	// Not takeoff → fail.
	r = e.Command(cs, "mlt")
	if r.OK {
		t.Error("mlt parked should fail")
	}

	// Put in takeoff.
	e.mu.Lock()
	ac := e.aircraft[cs]
	s := e.airport.FindSurface("33")
	thr, hdg, _ := runwayThreshold(s, "33")
	ac.Lat, ac.Lon = thr.Lat, thr.Lon
	ac.Heading = hdg
	ac.Status = StatusTakeoff
	ac.ClearedTakeoff = true
	ac.DepRunway = "33"
	e.mu.Unlock()

	r = e.Command(cs, "mlt")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).PatternTraffic != "L" {
		t.Error("mlt traffic")
	}
	r = e.Command(cs, "mrt")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).PatternTraffic != "R" {
		t.Error("mrt traffic")
	}
}

func TestCommand_MSA_MNA(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33")

	r := e.Command(cs, "msa")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if !e.mustGet(t, cs).ShortApproach {
		t.Error("ShortApproach")
	}
	r = e.Command(cs, "mna")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).ShortApproach {
		t.Error("ShortApproach should clear")
	}
}

func TestCommand_CallsignPrefix_Pattern(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	r := e.CommandLine(cs + ", eld 33")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).Status != StatusDownwind {
		t.Errorf("status = %s", e.mustGet(t, cs).Status)
	}
}

// --- Motion -----------------------------------------------------------------

func TestTick_PatternAdvancesLegs(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Small pattern + high speed so legs advance in reasonable ticks.
	e.Command(cs, "ps 0.5")
	e.Command(cs, "eld 33")
	e.Command(cs, "spd 150")

	e.Unpause()
	// Snap near downwind end so next ticks turn base.
	e.mu.Lock()
	ac := e.aircraft[cs]
	a, errMsg := e.anchorsForAircraftLocked(ac)
	if errMsg != "" {
		e.mu.Unlock()
		t.Fatal(errMsg)
	}
	// Place just before DownwindEnd along downwind heading.
	backBrg := normalizeHeading(a.legHeading(StatusDownwind) + 180)
	lat, lon := destinationPoint(a.DownwindEnd.Lat, a.DownwindEnd.Lon, backBrg, 100)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.legHeading(StatusDownwind)
	ac.Speed = 150
	ac.ExtendLeg = false
	e.mu.Unlock()

	sawBase := false
	for i := 0; i < 30; i++ {
		e.Tick(time.Second)
		st := e.mustGet(t, cs).Status
		if st == StatusBase || st == StatusFinal {
			sawBase = true
			break
		}
	}
	if !sawBase {
		t.Fatalf("expected advance to base/final; status=%s", e.mustGet(t, cs).Status)
	}
}

func TestTick_ExtendHoldsLeg(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "ps 0.5")
	e.Command(cs, "eld 33")
	e.Command(cs, "ext")
	e.Command(cs, "spd 150")

	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	// Place at the downwind corner — without extend would turn base.
	ac.Lat, ac.Lon = a.DownwindEnd.Lat, a.DownwindEnd.Lon
	ac.Heading = a.legHeading(StatusDownwind)
	ac.Speed = 150
	e.mu.Unlock()

	e.Unpause()
	for i := 0; i < 10; i++ {
		e.Tick(time.Second)
	}
	if e.mustGet(t, cs).Status != StatusDownwind {
		t.Fatalf("extend should hold downwind; status=%s", e.mustGet(t, cs).Status)
	}
	// tb forces base.
	if !e.Command(cs, "tb").OK {
		t.Fatal("tb")
	}
	if e.mustGet(t, cs).Status != StatusBase {
		t.Fatalf("status = %s", e.mustGet(t, cs).Status)
	}
}

func TestTick_FinalFullStop(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "ef 33")
	e.Command(cs, "fs")
	e.Command(cs, "spd 90")

	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	// Place very close to threshold on final.
	lat, lon := destinationPoint(a.Threshold.Lat, a.Threshold.Lon, normalizeHeading(a.LandingHdg+180), 50)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.LandingHdg
	ac.Alt = fieldElevFeet(e.airport) + 30
	ac.Speed = 90
	e.mu.Unlock()

	e.Unpause()
	for i := 0; i < 20; i++ {
		e.Tick(time.Second)
		if e.mustGet(t, cs).Status == StatusLanded {
			break
		}
	}
	acSnap := e.mustGet(t, cs)
	if acSnap.Status != StatusLanded {
		t.Fatalf("status = %s", acSnap.Status)
	}
	if e.Ops().ArrCount < 1 {
		t.Error("expected arr counter++")
	}
}

func TestTick_FinalTouchAndGo(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33")
	e.Command(cs, "tg")
	// Jump to short final.
	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	lat, lon := destinationPoint(a.Threshold.Lat, a.Threshold.Lon, normalizeHeading(a.LandingHdg+180), 40)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.LandingHdg
	ac.Status = StatusFinal
	ac.Alt = fieldElevFeet(e.airport) + 20
	ac.Speed = 80
	ac.LandingType = LandingTG
	e.mu.Unlock()

	e.Unpause()
	// Hit threshold → takeoff, then climb into upwind.
	var sawTO, sawUpwind bool
	for i := 0; i < 120; i++ {
		e.Tick(time.Second)
		st := e.mustGet(t, cs).Status
		if st == StatusTakeoff {
			sawTO = true
		}
		if st == StatusUpwind {
			sawUpwind = true
			break
		}
	}
	if !sawTO {
		t.Error("expected takeoff after TG")
	}
	if !sawUpwind {
		t.Fatalf("expected upwind after TG climb; status=%s alt=%.0f",
			e.mustGet(t, cs).Status, e.mustGet(t, cs).Alt)
	}
}

func TestTick_LowApproach(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "ef 33")
	e.Command(cs, "la")

	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	lat, lon := destinationPoint(a.Threshold.Lat, a.Threshold.Lon, normalizeHeading(a.LandingHdg+180), 40)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.LandingHdg
	ac.Alt = fieldElevFeet(e.airport) + 250
	ac.Speed = 90
	e.mu.Unlock()

	e.Unpause()
	for i := 0; i < 15; i++ {
		e.Tick(time.Second)
		if e.mustGet(t, cs).Status == StatusUpwind {
			break
		}
	}
	acSnap := e.mustGet(t, cs)
	if acSnap.Status != StatusUpwind {
		t.Fatalf("status = %s", acSnap.Status)
	}
	if !strings.Contains(strings.ToLower(acSnap.Instruction), "low approach") {
		t.Errorf("instruction = %q", acSnap.Instruction)
	}
}

func TestTick_StopAndGo_TimerAndGo(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "ef 33")
	e.Command(cs, "sg 3")

	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	lat, lon := destinationPoint(a.Threshold.Lat, a.Threshold.Lon, normalizeHeading(a.LandingHdg+180), 30)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.LandingHdg
	ac.Status = StatusFinal
	ac.Alt = fieldElevFeet(e.airport) + 15
	ac.Speed = 70
	e.mu.Unlock()

	e.Unpause()
	// Reach SG wait.
	for i := 0; i < 15; i++ {
		e.Tick(time.Second)
		if e.mustGet(t, cs).SGWaiting {
			break
		}
	}
	if !e.mustGet(t, cs).SGWaiting {
		t.Fatalf("expected SG wait; status=%s", e.mustGet(t, cs).Status)
	}
	// Timer 3s → roll.
	for i := 0; i < 5; i++ {
		e.Tick(time.Second)
	}
	st := e.mustGet(t, cs)
	if st.SGWaiting {
		t.Error("timer should clear SG wait")
	}
	if st.Status != StatusTakeoff && st.Status != StatusUpwind {
		t.Errorf("status after SG timer = %s", st.Status)
	}

	// Indefinite SG + go command.
	e.Command(cs, "ef 33")
	e.Command(cs, "sg")
	e.mu.Lock()
	ac = e.aircraft[cs]
	a, _ = e.anchorsForAircraftLocked(ac)
	lat, lon = destinationPoint(a.Threshold.Lat, a.Threshold.Lon, normalizeHeading(a.LandingHdg+180), 30)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.LandingHdg
	ac.Status = StatusFinal
	ac.LandingType = LandingSG
	ac.SGWaitSec = 0
	ac.Alt = fieldElevFeet(e.airport) + 15
	ac.Speed = 70
	e.mu.Unlock()
	for i := 0; i < 15; i++ {
		e.Tick(time.Second)
		if e.mustGet(t, cs).SGWaiting {
			break
		}
	}
	// Still waiting after several ticks.
	e.Tick(5 * time.Second)
	if !e.mustGet(t, cs).SGWaiting {
		t.Fatal("indefinite SG should still wait")
	}
	r := e.Command(cs, "go")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).SGWaiting {
		t.Error("go should clear wait")
	}
	if e.mustGet(t, cs).Status != StatusTakeoff {
		t.Errorf("status = %s", e.mustGet(t, cs).Status)
	}
}

func TestTick_ClosedTrafficTakeoffEntersPattern(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign

	e.mu.Lock()
	ac := e.aircraft[cs]
	s := e.airport.FindSurface("33")
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
	ac.LandingRunway = "33"
	e.mu.Unlock()

	r = e.Command(cs, "ctomlt")
	if !r.OK {
		t.Fatal(r.Message)
	}
	e.Unpause()
	// Climb to pattern alt (~1335) with piston; pattern climb 1000 fpm → ~1 min.
	var sawUpwind bool
	for i := 0; i < 200; i++ {
		e.Tick(time.Second)
		st := e.mustGet(t, cs)
		if st.Status == StatusUpwind {
			sawUpwind = true
			if st.PatternTraffic != "L" {
				t.Errorf("traffic = %s", st.PatternTraffic)
			}
			if !st.InPattern {
				t.Error("InPattern")
			}
			break
		}
		// Must not go to Departing when closed traffic.
		if st.Status == StatusDeparting {
			t.Fatal("closed traffic must not Departing")
		}
	}
	if !sawUpwind {
		t.Fatalf("expected Upwind; status=%s alt=%.0f", e.mustGet(t, cs).Status, e.mustGet(t, cs).Alt)
	}
}

func TestTick_MidfieldReport(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33")

	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	// Place slightly before midfield on downwind.
	back := normalizeHeading(a.legHeading(StatusDownwind) + 180)
	lat, lon := destinationPoint(a.MidfieldDW.Lat, a.MidfieldDW.Lon, back, 50)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.legHeading(StatusDownwind)
	ac.Speed = 100
	ac.MidfieldReported = false
	e.mu.Unlock()

	e.Unpause()
	for i := 0; i < 10; i++ {
		e.Tick(time.Second)
		if strings.Contains(e.mustGet(t, cs).Instruction, "Midfield") {
			return
		}
	}
	t.Fatalf("expected midfield report; instr=%q", e.mustGet(t, cs).Instruction)
}

func TestSnapshot_PatternFields(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33")
	e.Command(cs, "ps 1.5")
	e.Command(cs, "sg 10")
	e.Command(cs, "ext")
	e.Command(cs, "msa")

	snap := e.mustGet(t, cs)
	if !snap.InPattern || snap.PatternSizeNM != 1.5 || snap.LandingType != LandingSG {
		t.Errorf("snap pattern fields: %+v", snap)
	}
	if !snap.ExtendLeg || !snap.ShortApproach || snap.SGWaitSec != 10 {
		t.Errorf("snap flags: extend=%v short=%v sg=%v", snap.ExtendLeg, snap.ShortApproach, snap.SGWaitSec)
	}
}

func TestPattern_EffectiveSizeAndAlt(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	ac := e.aircraft[cs]
	if s := e.effectivePatternSizeNMLocked(ac); s != 1.0 {
		t.Errorf("default size = %v", s)
	}
	ac.PatternSizeNM = 2.5
	if s := e.effectivePatternSizeNMLocked(ac); s != 2.5 {
		t.Errorf("override size = %v", s)
	}
	e.mu.Unlock()
	if patternAltitudeMSL(nil) != 1000 {
		t.Error("nil apt alt")
	}
	if patternAltitudeMSL(e.Airport()) != 1335 {
		t.Errorf("KBTV pattern alt = %v", patternAltitudeMSL(e.Airport()))
	}
}

func TestGo_NotWaiting(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	r := e.Command(cs, "go")
	if r.OK {
		t.Error("go without SG wait should fail")
	}
}

func TestLegHelpers(t *testing.T) {
	if turnCommandTargetLeg("tc") != StatusCrosswind {
		t.Error("tc")
	}
	if turnCommandTargetLeg("tdn") != StatusDownwind {
		t.Error("tdn")
	}
	if turnCommandTargetLeg("tb") != StatusBase {
		t.Error("tb")
	}
	if turnCommandTargetLeg("xx") != "" {
		t.Error("xx")
	}
	leg, tr, ok := patternLegFromEnterVerb("elb")
	if !ok || leg != StatusBase || tr != "L" {
		t.Errorf("elb → %s %s", leg, tr)
	}
	if clampPatternSize(0.1) != minPatternSizeNM {
		t.Error("clamp lo")
	}
	if clampPatternSize(100) != maxPatternSizeNM {
		t.Error("clamp hi")
	}
	if formatLandingTypeName(LandingTG) == "" || formatLandingTypeName("X") != "X" {
		t.Error("landing type name")
	}
	for _, v := range []string{"erc", "erd", "erb", "elc", "eld", "elb", "ef"} {
		if _, _, ok := patternLegFromEnterVerb(v); !ok {
			t.Errorf("enter verb %s", v)
		}
	}
	if _, _, ok := patternLegFromEnterVerb("nope"); ok {
		t.Error("nope")
	}
	for _, name := range []string{LandingTG, LandingSG, LandingLA, LandingFS, "X"} {
		_ = formatLandingTypeName(name)
	}
}

func TestCommand_AllEnterVerbs(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	for _, cmd := range []struct {
		line string
		leg  string
		tr   string
	}{
		{"erc 15", StatusCrosswind, "R"},
		{"erd 15", StatusDownwind, "R"},
		{"erb 15", StatusBase, "R"},
		{"elc 33", StatusCrosswind, "L"},
		{"eld 33", StatusDownwind, "L"},
		{"elb 33", StatusBase, "L"},
		{"ef 33", StatusFinal, "L"},
	} {
		r := e.Command(cs, cmd.line)
		if !r.OK {
			t.Fatalf("%s: %s", cmd.line, r.Message)
		}
		ac := e.mustGet(t, cs)
		if ac.Status != cmd.leg {
			t.Errorf("%s status=%s want %s", cmd.line, ac.Status, cmd.leg)
		}
		if ac.PatternTraffic != cmd.tr {
			t.Errorf("%s traffic=%s want %s", cmd.line, ac.PatternTraffic, cmd.tr)
		}
	}
}

func TestCommand_EnterUpwindViaPlace(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	ac := e.aircraft[cs]
	s, end, errMsg := e.resolveRunwaySurfaceLocked("33")
	if errMsg != "" {
		e.mu.Unlock()
		t.Fatal(errMsg)
	}
	a, errMsg := computePatternAnchors(s, end, "L", 1)
	if errMsg != "" {
		e.mu.Unlock()
		t.Fatal(errMsg)
	}
	placeOnPatternLegLocked(ac, a, StatusUpwind, e.airport)
	// default / unknown leg falls through to downwind
	placeOnPatternLegLocked(ac, a, "Nope", e.airport)
	if ac.Status != StatusDownwind {
		t.Errorf("default place status=%s", ac.Status)
	}
	// cover legTarget default
	_ = a.legTarget("Nope")
	_ = a.legHeading("Nope")
	_ = nextLeg("Nope")
	_ = prevLeg("Nope")
	e.mu.Unlock()
}

func TestCommand_LandingType_Errors(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	// Parked — not eligible.
	r = e.Command(cs, "tg")
	if r.OK {
		t.Error("tg parked should fail")
	}
	// Bad sg arg.
	cs2 := addAirborne(t, e)
	r = e.Command(cs2, "sg -1")
	if r.OK {
		t.Error("sg negative should fail")
	}
	r = e.Command(cs2, "sg nope")
	if r.OK {
		t.Error("sg non-number should fail")
	}
}

func TestCommand_GoAround_EdgeCases(t *testing.T) {
	e := loadKBTVEngine(t)
	// Not in pattern.
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	r = e.Command(cs, "ga")
	if r.OK {
		t.Error("ga parked should fail")
	}

	// Airborne without runway → climb fallback.
	cs2 := addAirborne(t, e)
	e.mu.Lock()
	e.aircraft[cs2].LandingRunway = ""
	e.aircraft[cs2].DepRunway = ""
	e.aircraft[cs2].Status = StatusFinal
	e.aircraft[cs2].InPattern = true
	e.mu.Unlock()
	r = e.Command(cs2, "ga")
	if !r.OK {
		t.Fatal(r.Message)
	}
	// Upwind path from base.
	e.Command(cs2, "elb 33")
	r = e.Command(cs2, "ga")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs2).Status != StatusUpwind {
		t.Errorf("status=%s", e.mustGet(t, cs2).Status)
	}
}

func TestCommand_ShortApproach_NotInPattern(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	if e.Command(cs, "msa").OK {
		t.Error("msa parked should fail")
	}
}

func TestCommand_MakeTraffic_ClearedGround(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusHoldingShort
	ac.ClearedTakeoff = true
	ac.DepRunway = "33"
	e.mu.Unlock()
	r = e.Command(cs, "mrt")
	if !r.OK {
		t.Fatal(r.Message)
	}
	if e.mustGet(t, cs).PatternTraffic != "R" {
		t.Error("traffic")
	}
}

func TestCommand_TurnCrosswindDownwind(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Upwind then tc.
	e.mu.Lock()
	ac := e.aircraft[cs]
	s, end, _ := e.resolveRunwaySurfaceLocked("33")
	a, _ := computePatternAnchors(s, end, "L", 1)
	placeOnPatternLegLocked(ac, a, StatusUpwind, e.airport)
	e.mu.Unlock()
	if !e.Command(cs, "tc").OK {
		t.Fatal("tc")
	}
	if e.mustGet(t, cs).Status != StatusCrosswind {
		t.Fatal(e.mustGet(t, cs).Status)
	}
	if !e.Command(cs, "td").OK {
		t.Fatal("td")
	}
	if e.mustGet(t, cs).Status != StatusDownwind {
		t.Fatal(e.mustGet(t, cs).Status)
	}
	// td from wrong leg
	if e.Command(cs, "td").OK {
		t.Error("td from downwind should fail")
	}
}

func TestTick_ShortApproachCutsToFinal(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "eld 33")
	e.Command(cs, "msa")
	e.Command(cs, "spd 120")

	e.mu.Lock()
	ac := e.aircraft[cs]
	a, _ := e.anchorsForAircraftLocked(ac)
	// At midfield → short approach should cut to final.
	ac.Lat, ac.Lon = a.MidfieldDW.Lat, a.MidfieldDW.Lon
	ac.Heading = a.legHeading(StatusDownwind)
	ac.Speed = 120
	ac.MidfieldReported = false
	e.mu.Unlock()

	e.Unpause()
	for i := 0; i < 15; i++ {
		e.Tick(time.Second)
		if e.mustGet(t, cs).Status == StatusFinal {
			return
		}
	}
	t.Fatalf("expected final via short approach; status=%s", e.mustGet(t, cs).Status)
}

func TestTick_PatternUpwindToCrosswind(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.Command(cs, "ps 0.5")
	e.mu.Lock()
	ac := e.aircraft[cs]
	s, end, _ := e.resolveRunwaySurfaceLocked("33")
	a, _ := computePatternAnchors(s, end, "L", 0.5)
	placeOnPatternLegLocked(ac, a, StatusUpwind, e.airport)
	// Near upwind end.
	back := normalizeHeading(a.LandingHdg + 180)
	lat, lon := destinationPoint(a.UpwindEnd.Lat, a.UpwindEnd.Lon, back, 80)
	ac.Lat, ac.Lon = lat, lon
	ac.Heading = a.LandingHdg
	ac.Speed = 120
	ac.ExtendLeg = false
	e.mu.Unlock()

	e.Unpause()
	for i := 0; i < 20; i++ {
		e.Tick(time.Second)
		if e.mustGet(t, cs).Status == StatusCrosswind {
			return
		}
	}
	t.Fatalf("expected crosswind; status=%s", e.mustGet(t, cs).Status)
}

func TestAnchorsForAircraft_Errors(t *testing.T) {
	e := loadKBTVEngine(t)
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, msg := e.anchorsForAircraftLocked(nil); msg == "" {
		t.Error("nil ac")
	}
	ac := &SimAircraft{Callsign: "Z"}
	if _, msg := e.anchorsForAircraftLocked(ac); msg == "" {
		t.Error("no runway")
	}
	ac.LandingRunway = "99"
	if _, msg := e.anchorsForAircraftLocked(ac); msg == "" {
		t.Error("bad runway")
	}
	if distToPoint(nil, Point{}) == 0 {
		t.Error("dist nil")
	}
	// size clamp branches
	ac.PatternSizeNM = 100
	ac.LandingRunway = "33"
	a, msg := e.anchorsForAircraftLocked(ac)
	if msg != "" {
		t.Fatal(msg)
	}
	if a.SizeNM != maxPatternSizeNM {
		t.Errorf("size clamped to %v", a.SizeNM)
	}
	ac.PatternSizeNM = 0.1 // below min → use airport
	if e.effectivePatternSizeNMLocked(ac) != 1.0 {
		t.Errorf("size = %v", e.effectivePatternSizeNMLocked(ac))
	}
	// oversize airport size
	old := e.airport.PatternSize
	e.airport.PatternSize = 50
	ac.PatternSizeNM = 0
	if e.effectivePatternSizeNMLocked(ac) != maxPatternSizeNM {
		t.Errorf("apt size clamp = %v", e.effectivePatternSizeNMLocked(ac))
	}
	e.airport.PatternSize = old
}

func TestEnterPatternFromTakeoff_NoRunway(t *testing.T) {
	e := loadKBTVEngine(t)
	e.mu.Lock()
	defer e.mu.Unlock()
	ac := &SimAircraft{PatternTraffic: "L"}
	e.enterPatternFromTakeoffLocked(ac) // no-op
	e.enterPatternFromTakeoffLocked(nil)
	ac.DepRunway = "99"
	e.enterPatternFromTakeoffLocked(ac) // bad rwy
}

func TestClimbTowardRate(t *testing.T) {
	ac := &SimAircraft{Alt: 1000}
	// Already at target.
	if climbTowardRate(ac, 1000, 1000, 1) {
		// may still return true if forced assign; either way ok
	}
	ac.Alt = 1000
	if !climbTowardRate(ac, 2000, 0, 1) { // rate 0 → default climbRateFpm
		t.Error("climb")
	}
	if ac.Alt <= 1000 {
		t.Error("should climb")
	}
	// step = 60000 fpm * 1s / 60 = 1000 ft → snap to target
	ac.Alt = 2000
	if !climbTowardRate(ac, 1000, 60000, 1) {
		t.Error("descend")
	}
	if ac.Alt != 1000 {
		t.Errorf("snap desc = %v", ac.Alt)
	}
	// Within equality epsilon: assign exact target.
	ac.Alt = 1000.1
	if !climbTowardRate(ac, 1000, 1000, 1) {
		t.Error("eps")
	}
	if ac.Alt != 1000 {
		t.Errorf("alt=%v", ac.Alt)
	}
}

func TestFormatPatternInstruction_Variants(t *testing.T) {
	if formatPatternInstruction(nil) != "" {
		t.Error("nil")
	}
	ac := &SimAircraft{
		Status:         StatusFinal,
		LandingRunway:  "15",
		PatternTraffic: "R",
		ExtendLeg:      true,
		ShortApproach:  true,
		LandingType:    LandingFS,
	}
	s := formatPatternInstruction(ac)
	if !strings.Contains(s, "Final") || !strings.Contains(s, "right") ||
		!strings.Contains(s, "extending") || !strings.Contains(s, "short") ||
		!strings.Contains(s, "full stop") {
		t.Errorf("instr=%q", s)
	}
	ac.Status = StatusAirborne
	ac.InPattern = true
	_ = formatPatternInstruction(ac)
	ac.InPattern = false
	ac.Instruction = "keep"
	if formatPatternInstruction(ac) != "keep" {
		t.Error("preserve")
	}
	for _, lt := range []string{LandingTG, LandingSG, LandingLA, LandingFS} {
		ac.Status = StatusDownwind
		ac.LandingType = lt
		_ = formatPatternInstruction(ac)
	}
}

func TestInPatternLeg(t *testing.T) {
	if (*SimAircraft)(nil).inPatternLeg() {
		t.Error("nil")
	}
	ac := &SimAircraft{Status: StatusDownwind}
	if !ac.inPatternLeg() {
		t.Error("downwind")
	}
	ac.Status = StatusParked
	if ac.inPatternLeg() {
		t.Error("parked")
	}
}

func TestPatternAltitude_FieldOnly(t *testing.T) {
	apt := &Airport{FieldElev: 500, PatternElev: 0}
	if patternAltitudeMSL(apt) != 1500 {
		t.Errorf("alt=%v", patternAltitudeMSL(apt))
	}
}

func TestTick_PatternNoAnchorsFallsBack(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusDownwind
	ac.InPattern = true
	ac.LandingRunway = ""
	ac.DepRunway = ""
	ac.Speed = 100
	ac.HasDesiredHeading = true
	ac.DesiredHeading = 90
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second) // should not panic; falls back to airborne
}

func TestResolveRunway_CombinedName(t *testing.T) {
	e := loadKBTVEngine(t)
	e.mu.Lock()
	defer e.mu.Unlock()
	s, end, err := e.resolveRunwaySurfaceLocked("33/15")
	if err != "" || s == nil {
		t.Fatalf("err=%s", err)
	}
	if end != s.RwyA {
		t.Errorf("end=%s want %s", end, s.RwyA)
	}
	_, _, err = e.resolveRunwaySurfaceLocked("")
	if err == "" {
		t.Error("empty")
	}
}

func TestLandingType_InstructionWithoutPattern(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	// Departing with landing runway.
	e.mu.Lock()
	e.aircraft[cs].Status = StatusDeparting
	e.aircraft[cs].LandingRunway = "33"
	e.mu.Unlock()
	if !e.Command(cs, "tg").OK {
		t.Fatal("tg")
	}
	if !strings.Contains(e.mustGet(t, cs).Instruction, "Touch") {
		t.Errorf("instr=%q", e.mustGet(t, cs).Instruction)
	}
}

func TestGo_DefaultsTraffic(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.SGWaiting = true
	ac.Status = StatusLanded
	ac.LandingRunway = "33"
	ac.PatternTraffic = ""
	e.mu.Unlock()
	if !e.Command(cs, "go").OK {
		t.Fatal("go")
	}
	if e.mustGet(t, cs).PatternTraffic != "L" {
		t.Error("default L")
	}
}

func TestNormalizeVerb_PatternAliases(t *testing.T) {
	if normalizeVerb("tcn") != "tc" || normalizeVerb("tdn") != "td" || normalizeVerb("tbn") != "tb" {
		t.Error("aliases")
	}
	// exercise via command line
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	s, end, _ := e.resolveRunwaySurfaceLocked("33")
	a, _ := computePatternAnchors(s, end, "L", 1)
	placeOnPatternLegLocked(e.aircraft[cs], a, StatusUpwind, e.airport)
	e.mu.Unlock()
	if !e.Command(cs, "tcn").OK {
		t.Fatal("tcn")
	}
}

func TestShortApproach_OnApproach(t *testing.T) {
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p 33 4")
	cs := r.Added[0].Callsign
	if !e.Command(cs, "msa").OK {
		t.Fatal("msa on approach")
	}
	if !e.mustGet(t, cs).ShortApproach {
		t.Error("short")
	}
	if !e.Command(cs, "mna").OK {
		t.Fatal("mna")
	}
}

func TestComputeAnchors_SizeClampTrafficDefault(t *testing.T) {
	e := loadKBTVEngine(t)
	s := e.Airport().FindSurface("19")
	a, err := computePatternAnchors(s, "19", "X", 0.1) // bad traffic → L, size clamp min
	if err != "" {
		t.Fatal(err)
	}
	if a.Traffic != "L" || a.SizeNM != minPatternSizeNM {
		t.Errorf("traffic=%s size=%v", a.Traffic, a.SizeNM)
	}
	a, err = computePatternAnchors(s, "19", "R", 100)
	if err != "" {
		t.Fatal(err)
	}
	if a.SizeNM != maxPatternSizeNM {
		t.Errorf("size=%v", a.SizeNM)
	}
	// combined name end
	a, err = computePatternAnchors(s, s.Name, "L", 1)
	if err != "" {
		t.Fatal(err)
	}
	if a.Runway != s.RwyA {
		t.Errorf("runway=%s", a.Runway)
	}
}

func TestSGWait_ClearsSpeed(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	ac := e.aircraft[cs]
	ac.Status = StatusLanded
	ac.SGWaiting = true
	ac.SGWaitSec = 2
	ac.SGTimer = 2
	ac.Speed = 15
	ac.LandingRunway = "33"
	ac.PatternTraffic = "L"
	e.mu.Unlock()
	e.Unpause()
	e.Tick(time.Second)
	if e.mustGet(t, cs).Speed != 0 {
		t.Error("speed should freeze at 0")
	}
	// expire
	e.Tick(2 * time.Second)
	st := e.mustGet(t, cs)
	if st.SGWaiting || st.Status != StatusTakeoff {
		t.Errorf("after timer: waiting=%v status=%s", st.SGWaiting, st.Status)
	}
}

func TestLandingType_NoRunwayInstruction(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	e.mu.Lock()
	e.aircraft[cs].Status = StatusAirborne
	e.aircraft[cs].LandingRunway = ""
	e.aircraft[cs].DepRunway = ""
	e.mu.Unlock()
	if !e.Command(cs, "la").OK {
		t.Fatal("la")
	}
	if e.mustGet(t, cs).Instruction != "Low approach" {
		t.Errorf("instr=%q", e.mustGet(t, cs).Instruction)
	}
}

func TestPatternSize_BadArg(t *testing.T) {
	e := loadKBTVEngine(t)
	cs := addAirborne(t, e)
	if e.Command(cs, "ps abc").OK {
		t.Error("bad float")
	}
}

func TestApproachAltitude_Negative(t *testing.T) {
	if approachAltitude(100, -1) < 100 {
		t.Error("floor")
	}
}

func TestHoldNames_ViaRes(t *testing.T) {
	// Cover holdNames used in res instruction rebuild.
	e := loadKBTVEngine(t)
	r := e.CommandLine("add v s p @GA9")
	cs := r.Added[0].Callsign
	placeOnSurface(t, e, cs, "A")
	if !e.Command(cs, "taxi B 19 hs 19").OK {
		t.Fatal("taxi")
	}
	e.Unpause()
	// force hold present-position then res
	e.Command(cs, "hold")
	if !e.Command(cs, "res").OK {
		t.Fatal("res")
	}
	// also empty holdNames
	if holdNames(nil) != nil {
		t.Error("nil holds")
	}
}
