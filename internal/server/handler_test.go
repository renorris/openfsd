package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/auth"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// memRegistry is a thin adapter so tests can use postoffice as Registry.
type memRegistry struct {
	*postoffice.PostOffice
}

func newHandlerEnv(t *testing.T) (*Server, *memRegistry) {
	t.Helper()
	reg := &memRegistry{PostOffice: postoffice.New()}
	metar := &recordingMetar{}
	srv, err := New(Deps{
		Config:   &Config{FsdListenAddrs: []string{":0"}},
		Users:    stubUserStore{},
		ConfigKV: stubConfigStore{},
		Registry: reg,
		Metar:    metar,
		Clock:    fixedClock{t: time.Unix(1_700_000_000, 0)},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// stash metar for assertions
	srv.metar = metar
	return srv, reg
}

type recordingMetar struct {
	mu   sync.Mutex
	icao []string
}

func (m *recordingMetar) Request(ctx context.Context, s session.Sender, icao string) {
	m.mu.Lock()
	m.icao = append(m.icao, icao)
	m.mu.Unlock()
}
func (m *recordingMetar) Run(ctx context.Context) {}

func (m *recordingMetar) lastICAO() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.icao) == 0 {
		return ""
	}
	return m.icao[len(m.icao)-1]
}

func newSess(callsign string, isATC bool, rating NetworkRating) *session.Session {
	return session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      callsign,
		IsAtc:         isATC,
		NetworkRating: protocol.NetworkRating(rating),
		ProtoRevision: 101,
		CID:           1,
		RealName:      "Test",
	})
}

func newSessWithConn(callsign string, isATC bool, rating NetworkRating, conn net.Conn) *session.Session {
	s := session.New(context.Background(), conn, nil, session.LoginData{
		Callsign:      callsign,
		IsAtc:         isATC,
		NetworkRating: protocol.NetworkRating(rating),
		ProtoRevision: 101,
		CID:           1,
		RealName:      "Test",
	})
	return s
}

func drain(s *session.Session) []string {
	var out []string
	for {
		p, ok := s.DequeueOutbound()
		if !ok {
			break
		}
		out = append(out, p)
	}
	return out
}

func TestGetHandlerRouting(t *testing.T) {
	srv, _ := newHandlerEnv(t)
	cases := []struct {
		pt   PacketType
		want string
	}{
		{PacketTypeTextMessage, "handleTextMessage"},
		{PacketTypeATCPosition, "handleATCPosition"},
		{PacketTypeSecondaryVisCenter, "handleSecondaryVisCenter"},
		{PacketTypePilotPosition, "handlePilotPosition"},
		{PacketTypePilotPositionFast, "handleFastPilotPosition"},
		{PacketTypePilotPositionSlow, "handleFastPilotPosition"},
		{PacketTypePilotPositionStopped, "handleFastPilotPosition"},
		{PacketTypeDeletePilot, "handleDelete"},
		{PacketTypeDeleteATC, "handleDelete"},
		{PacketTypeSquawkbox, "handleSquawkbox"},
		{PacketTypeProController, "handleProcontroller"},
		{PacketTypeClientQuery, "handleClientQuery"},
		{PacketTypeClientQueryResponse, "handleClientQuery"},
		{PacketTypeKillRequest, "handleKillRequest"},
		{PacketTypeAuthChallenge, "handleAuthChallenge"},
		{PacketTypeHandoffRequest, "handleHandoff"},
		{PacketTypeHandoffAccept, "handleHandoff"},
		{PacketTypeMetarRequest, "handleMetarRequest"},
		{PacketTypeFlightPlan, "handleFileFlightplan"},
		{PacketTypeFlightPlanAmendment, "handleAmendFlightplan"},
		{PacketTypeUnknown, "emptyHandler"},
	}
	// We only assert non-nil handlers for each type (routing works).
	for _, tc := range cases {
		h := srv.getHandler(tc.pt)
		if h == nil {
			t.Fatalf("getHandler(%v) nil", tc.pt)
		}
	}
}

func TestEmptyHandler(t *testing.T) {
	srv, _ := newHandlerEnv(t)
	s := newSess("N1", false, NetworkRatingObserver)
	// Should not panic
	srv.emptyHandler(s, []byte("???\r\n"))
}

func TestHandleTextMessagePaths(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	pilot := newSess("N100", false, NetworkRatingObserver)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	sup := newSess("SUP1", true, NetworkRatingSupervisor)
	other := newSess("N200", false, NetworkRatingObserver)
	// Register at default (0,0), then UpdatePosition so the R-tree is rewritten
	// (pre-setting LatLon/VisRange to the same center would skip the tree rewrite).
	for _, s := range []*session.Session{pilot, atc, sup, other} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34.0, -118.0}, 50*1852)
	}

	// ATC chat — pilot ignored (not ATC)
	srv.handleTextMessage(pilot, []byte("#TMN100:@49999:hi\r\n"))
	requireNoOutbound(t, pilot, "pilot ATC-chat should not enqueue")

	// ATC chat — atc broadcasts to in-range ATC (sup receives)
	srv.handleTextMessage(atc, []byte("#TMLAX_TWR:@49999:atcchat\r\n"))
	outSup := drain(sup)
	if !hasOutboundContaining(outSup, "atcchat") {
		t.Fatalf("ATC chat not delivered to SUP1: %v", outSup)
	}

	// Direct message
	srv.handleTextMessage(pilot, []byte("#TMN100:N200:hello\r\n"))
	outOther := drain(other)
	if !hasOutboundContaining(outOther, "#TMN100:N200:hello") {
		t.Fatalf("direct message not delivered to N200: %v", outOther)
	}

	// Missing recipient → $ER to sender
	srv.handleTextMessage(pilot, []byte("#TMN100:NOSUCH:hello\r\n"))
	outPilot := drain(pilot)
	if !hasOutboundContaining(outPilot, "$ER") {
		t.Fatalf("expected $ER for missing callsign, got %v", outPilot)
	}

	// Frequency / wallop / server-wide paths (smoke + drain)
	srv.handleTextMessage(pilot, []byte("#TMN100:@12345:freq\r\n"))
	srv.handleTextMessage(pilot, []byte("#TMN100:*S:help\r\n"))
	srv.handleTextMessage(pilot, []byte("#TMN100:*:all\r\n")) // non-sup no-op
	srv.handleTextMessage(sup, []byte("#TMSUP1:*:all\r\n"))
	srv.handleTextMessage(pilot, []byte("#TMN100:FP:x\r\n"))
	srv.handleTextMessage(pilot, []byte("#TMN100:SERVER:x\r\n"))
	_ = drain(pilot)
	_ = drain(atc)
	_ = drain(sup)
	_ = drain(other)
}

func requireNoOutbound(t *testing.T, s *session.Session, msg string) {
	t.Helper()
	if out := drain(s); len(out) != 0 {
		t.Fatalf("%s: got %v", msg, out)
	}
}

func hasOutboundContaining(packets []string, substr string) bool {
	for _, p := range packets {
		if strings.Contains(p, substr) {
			return true
		}
	}
	return false
}

func TestHandleATCPosition(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}

	// Valid DEL facility (2) for C1
	pkt := []byte("%LAX_TWR:28550:2:50:1:34.0:-118.0:0\r\n")
	srv.handleATCPosition(atc, pkt)
	if atc.FacilityType.Load() != 2 {
		t.Fatalf("FacilityType=%d want 2", atc.FacilityType.Load())
	}
	if atc.Frequency.Load() != "28550" {
		t.Fatalf("Frequency=%q", atc.Frequency.Load())
	}

	// Invalid facility type parse
	srv.handleATCPosition(atc, []byte("%LAX_TWR:28550:xx:50:1:34.0:-118.0:0\r\n"))
	// Bad lat
	srv.handleATCPosition(atc, []byte("%LAX_TWR:28550:2:50:1:xx:-118.0:0\r\n"))
	// Bad vis range
	srv.handleATCPosition(atc, []byte("%LAX_TWR:28550:2:xx:1:34.0:-118.0:0\r\n"))

	// OBS rating cannot hold CTR (facility 6)
	obs := newSess("OBS1", true, NetworkRatingObserver)
	if err := reg.Register(obs); err != nil {
		t.Fatal(err)
	}
	srv.handleATCPosition(obs, []byte("%OBS1:28550:6:50:1:34.0:-118.0:0\r\n"))
	// Cancel should have been called
	select {
	case <-obs.Ctx.Done():
	default:
		t.Fatal("expected Cancel after invalid position for rating")
	}
}

func TestHandleSecondaryVisCenter(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	atc := newSess("LAX_CTR", true, NetworkRatingController1)
	if err := reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	// Primary position first (sets VisRange used by secondary boxes).
	// Clears secondaries after broadcast (none yet).
	srv.handleATCPosition(atc, []byte("%LAX_CTR:28550:6:40:5:34.0:-118.0:0\r\n"))
	if atc.SecondaryVisCenterCount() != 0 {
		t.Fatalf("expected no secondaries, got %d", atc.SecondaryVisCenterCount())
	}

	// Valid SECPOS secondary (index 0).
	srv.handleSecondaryVisCenter(atc, []byte("'LAX_CTR:0:36.00000:-118.00000\r\n"))
	if atc.SecondaryVisCenterCount() != 1 {
		t.Fatalf("secondary count=%d want 1", atc.SecondaryVisCenterCount())
	}

	// Another index.
	srv.handleSecondaryVisCenter(atc, []byte("'LAX_CTR:1:35.50000:-117.50000\r\n"))
	if atc.SecondaryVisCenterCount() != 2 {
		t.Fatalf("secondary count=%d want 2", atc.SecondaryVisCenterCount())
	}

	// Next % fans out with current secondaries, then clears (vatSys re-sends ').
	srv.handleATCPosition(atc, []byte("%LAX_CTR:28550:6:40:5:34.0:-118.0:0\r\n"))
	if atc.SecondaryVisCenterCount() != 0 {
		t.Fatalf("expected clear after %% fan-out, got %d", atc.SecondaryVisCenterCount())
	}

	// Out-of-range index ignored.
	srv.handleSecondaryVisCenter(atc, []byte("'LAX_CTR:99:36.0:-118.0\r\n"))
	if atc.SecondaryVisCenterCount() != 0 {
		t.Fatalf("out-of-range index should not set center, count=%d", atc.SecondaryVisCenterCount())
	}

	// Pilot emitting SECPOS: silent drop.
	pilot := newSess("N1", false, NetworkRatingObserver)
	if err := reg.Register(pilot); err != nil {
		t.Fatal(err)
	}
	srv.handleSecondaryVisCenter(pilot, []byte("'N1:0:34.0:-118.0\r\n"))
	if pilot.SecondaryVisCenterCount() != 0 {
		t.Fatal("pilot must not store secondary centers")
	}

	// Bad parse → $ER
	drain(atc)
	srv.handleSecondaryVisCenter(atc, []byte("'LAX_CTR:0:bad:-118.0\r\n"))
	out := drain(atc)
	if !hasOutboundContaining(out, "$ER") {
		t.Fatalf("expected $ER on bad lat, got %v", out)
	}
}

func TestHandlePilotPositionAndFast(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	p1 := newSess("N100", false, NetworkRatingObserver)
	p2 := newSess("N200", false, NetworkRatingObserver)
	p2.ProtoRevision = 101
	for _, s := range []*session.Session{p1, p2} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	reg.UpdatePosition(p1, [2]float64{34.0, -118.0}, 50*1852)
	reg.UpdatePosition(p2, [2]float64{34.001, -118.001}, 50*1852)

	// Wire format: @MODE:CALLSIGN:XPDR:RATING:LAT:LON:ALT:GS:PBH:CORR
	pkt := []byte("@S:N100:1200:1:34.0:-118.0:5000:250:4261294148:0\r\n")
	p1.ProtoRevision = 101
	// ClosestVelocityClientDistance is rewritten by Search during broadcastRanged.
	// Place p2 nearby so Search reports a close velocity peer and enables $SF.
	srv.handlePilotPosition(p1, pkt)
	if p1.Altitude.Load() != 5000 {
		t.Fatalf("alt=%d", p1.Altitude.Load())
	}
	if p1.Transponder.Load() != "1200" {
		t.Fatalf("xpdr=%q", p1.Transponder.Load())
	}
	// Nearby p2 (proto 101 pilot) should enable send-fast.
	if !p1.SendFastEnabled {
		t.Fatalf("expected SendFastEnabled true (closest=%.0fm)", p1.ClosestVelocityClientDistance)
	}
	out := drain(p1)
	foundSF := false
	for _, o := range out {
		if strings.Contains(o, "$SF") && strings.Contains(o, ":1\r\n") {
			foundSF = true
		}
	}
	if !foundSF {
		t.Fatalf("expected $SF enable, got %v", out)
	}

	// Move p2 far away so next position disables send-fast.
	reg.UpdatePosition(p2, [2]float64{40.0, -100.0}, 50*1852)
	srv.handlePilotPosition(p1, pkt)
	if p1.SendFastEnabled {
		t.Fatalf("expected SendFastEnabled false (closest=%.0fm)", p1.ClosestVelocityClientDistance)
	}

	// Invalid lat
	srv.handlePilotPosition(p1, []byte("@S:N100:1200:1:xx:-118.0:5000:250:0:0\r\n"))

	// Fast position broadcast
	srv.handleFastPilotPosition(p1, []byte("^N100:34.0:-118.0:5000:250:0:0:0:0:0:0:0:0\r\n"))
}

func TestHandleDeleteSquawkboxProcontroller(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	p := newSess("N100", false, NetworkRatingObserver)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	atc.FacilityType.Store(4)
	other := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{p, atc, other} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}

	srv.handleDelete(p, []byte("#DPN100:100\r\n"))
	select {
	case <-p.Ctx.Done():
	default:
		t.Fatal("delete should cancel")
	}

	// Squawkbox direct
	srv.handleSquawkbox(atc, []byte("#SBLAX_TWR:N200:P2P:hi\r\n"))

	// Procontroller pilot ignored
	srv.handleProcontroller(other, []byte("#PCN200:LAX_TWR:CCP:VER\r\n"))

	// Procontroller unprivileged VER
	srv.handleProcontroller(atc, []byte("#PCLAX_TWR:N200:CCP:VER\r\n"))

	// Procontroller invalid recipient
	srv.handleProcontroller(atc, []byte("#PCLAX_TWR:X:CCP:VER\r\n"))

	// Privileged without facility
	atc.FacilityType.Store(0)
	srv.handleProcontroller(atc, []byte("#PCLAX_TWR:N200:CCP:SC:ABC\r\n"))

	// Privileged with facility + range ATC
	atc.FacilityType.Store(4)
	srv.handleProcontroller(atc, []byte("#PCLAX_TWR:@94835:CCP:SC:ABC\r\n"))
	// Privileged direct
	srv.handleProcontroller(atc, []byte("#PCLAX_TWR:N200:CCP:BC:N200:1200\r\n"))
}

func TestHandleClientQuery(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	pilot := newSess("N100", false, NetworkRatingObserver)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	atc.FacilityType.Store(4)
	sup := newSess("SUP1", true, NetworkRatingSupervisor)
	target := newSess("N200", false, NetworkRatingObserver)
	target.FlightPlan.Store("I:B738:KLAX:KSFO")
	target.AssignedBeaconCode.Store("1234")
	obsATC := newSess("OBS1", true, NetworkRatingObserver)
	obsATC.FacilityType.Store(0)

	// Conn for IP query
	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	go io.Copy(io.Discard, c2)
	// Replace pilot with conn-backed for IP
	pilotIP := newSessWithConn("N100IP", false, NetworkRatingObserver, c1)

	for _, s := range []*session.Session{pilot, atc, sup, target, obsATC, pilotIP} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 50*1852)
	}

	// SERVER ATC query — active facility → Y
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:ATC:LAX_TWR\r\n"))
	outATC := drain(atc)
	if !hasOutboundContaining(outATC, "ATC:Y:") {
		t.Fatalf("expected ATC:Y response, got %v", outATC)
	}
	// SERVER ATC query — OBS facility → N
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:ATC:OBS1\r\n"))
	outATC = drain(atc)
	if !hasOutboundContaining(outATC, "ATC:N:") {
		t.Fatalf("expected ATC:N response, got %v", outATC)
	}
	// SERVER ATC missing
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:ATC:NONE\r\n"))
	if !hasOutboundContaining(drain(atc), "$ER") {
		t.Fatal("expected $ER for missing ATC target")
	}
	// SERVER ATC bad field count
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:ATC\r\n"))
	_ = drain(atc)

	// SERVER CAPS (query → advertised payload; includes SECPOS)
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:CAPS\r\n"))
	outCAPS := drain(atc)
	wantCAPS := "$CRSERVER:LAX_TWR:CAPS:" + ServerCapabilitiesPayload() + "\r\n"
	if !hasOutboundContaining(outCAPS, wantCAPS) {
		t.Fatalf("expected CAPS response %q, got %v", wantCAPS, outCAPS)
	}
	if !hasOutboundContaining(outCAPS, "SECPOS=1") {
		t.Fatalf("server CAPS must advertise SECPOS=1, got %v", outCAPS)
	}
	// SERVER CAPS client announce ($CR) is accepted and ignored (no reply, no $ER)
	srv.handleClientQuery(atc, []byte("$CRLAX_TWR:SERVER:CAPS:VERSION=1:ATCINFO=1\r\n"))
	if out := drain(atc); len(out) != 0 {
		t.Fatalf("client CAPS announce to SERVER should be silent, got %v", out)
	}

	// SERVER IP
	srv.handleClientQuery(pilotIP, []byte("$CQN100IP:SERVER:IP\r\n"))
	outIP := drain(pilotIP)
	if !hasOutboundContaining(outIP, "$CRSERVER:") || !hasOutboundContaining(outIP, ":IP:") {
		t.Fatalf("expected IP response, got %v", outIP)
	}
	// SERVER FP as pilot (ignored)
	srv.handleClientQuery(pilot, []byte("$CQN100:SERVER:FP:N200\r\n"))
	// SERVER FP as ATC
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:FP:N200\r\n"))
	outATC = drain(atc)
	if !hasOutboundContaining(outATC, "$FP") {
		t.Fatalf("expected $FP for flightplan request, got %v", outATC)
	}
	// SERVER FP no plan
	empty := newSess("N300", false, NetworkRatingObserver)
	_ = reg.Register(empty)
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:FP:N300\r\n"))
	// SERVER FP missing callsign
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:FP:ZZZZ\r\n"))
	// SERVER FP bad fields
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:SERVER:FP\r\n"))

	// Unprivileged ATC query from pilot → error
	srv.handleClientQuery(pilot, []byte("$CQN100:@94835:BY\r\n"))
	if !hasOutboundContaining(drain(pilot), "$ER") {
		t.Fatal("pilot BY query should $ER")
	}
	// Unprivileged from ATC
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:@94835:BY\r\n"))
	// Privileged from OBS facility → error
	srv.handleClientQuery(obsATC, []byte("$CQOBS1:N200:IT\r\n"))
	if !hasOutboundContaining(drain(obsATC), "$ER") {
		t.Fatal("OBS facility IT should $ER")
	}
	// Privileged OK
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:N200:IT\r\n"))
	// ACC from any
	srv.handleClientQuery(pilot, []byte("$CQN100:N200:ACC\r\n"))
	// INF interrogation without SUP
	srv.handleClientQuery(pilot, []byte("$CQN100:N200:INF\r\n"))
	// INF as SUP
	srv.handleClientQuery(sup, []byte("$CQSUP1:N200:INF\r\n"))
	// INF response
	srv.handleClientQuery(pilot, []byte("$CRN100:N200:INF:data\r\n"))
	// forward invalid recipient
	srv.handleClientQuery(pilot, []byte("$CQN100:X:ACC\r\n"))
	// range broadcasts
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:@94835:NEWATIS:A\r\n"))
	srv.handleClientQuery(atc, []byte("$CQLAX_TWR:@94836:NEWINFO:B\r\n"))

	_ = drain(atc)
	_ = drain(pilotIP)
}

func TestHandleMetarKillAuthHandoffFlightplan(t *testing.T) {
	srv, reg := newHandlerEnv(t)
	pilot := newSess("N100", false, NetworkRatingObserver)
	atc := newSess("LAX_TWR", true, NetworkRatingController1)
	atc.FacilityType.Store(4)
	sup := newSess("SUP1", true, NetworkRatingSupervisor)
	victim := newSess("N200", false, NetworkRatingObserver)
	for _, s := range []*session.Session{pilot, atc, sup, victim} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}

	// METAR ignore wrong recipient
	srv.handleMetarRequest(pilot, []byte("$AXN100:OTHER:METAR:KJFK\r\n"))
	// METAR OK
	srv.handleMetarRequest(pilot, []byte("$AXN100:SERVER:METAR:KJFK\r\n"))
	if rm, ok := srv.metar.(*recordingMetar); ok {
		if rm.lastICAO() != "KJFK" {
			t.Fatalf("icao=%q", rm.lastICAO())
		}
	}

	// Kill non-sup ignored
	srv.handleKillRequest(pilot, []byte("$!!N100:N200\r\n"))
	// Kill missing
	srv.handleKillRequest(sup, []byte("$!!SUP1:NONE\r\n"))
	// Kill OK
	srv.handleKillRequest(sup, []byte("$!!SUP1:N200\r\n"))
	select {
	case <-victim.Ctx.Done():
	default:
		t.Fatal("victim not cancelled")
	}

	// Auth challenge without init
	srv.handleAuthChallenge(pilot, []byte("$ZCN100:SERVER:abcdef\r\n"))
	// With auth state
	as := &auth.AuthState{}
	if err := as.Initialize(35044, []byte("0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	pilot.Auth = as
	pilot.ClientChallenge = "0123456789abcdef"
	srv.handleAuthChallenge(pilot, []byte("$ZCN100:SERVER:fedcba9876543210\r\n"))
	out := drain(pilot)
	found := false
	for _, o := range out {
		if strings.HasPrefix(o, "$ZRSERVER:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected $ZR, got %v", out)
	}

	// Handoff low facility ignored
	atc.FacilityType.Store(0)
	srv.handleHandoff(atc, []byte("$HOLAX_TWR:N100:CS\r\n"))
	atc.FacilityType.Store(4)
	srv.handleHandoff(atc, []byte("$HOLAX_TWR:N100:CS\r\n"))
	// pilot handoff ignored
	srv.handleHandoff(pilot, []byte("$HON100:LAX_TWR:CS\r\n"))

	// File flightplan
	srv.handleFileFlightplan(pilot, []byte("$FPN100:*A:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n"))
	if pilot.FlightPlan.Load() == "" {
		t.Fatal("flight plan not stored")
	}

	// Amend flightplan — non ATC ignored
	srv.handleAmendFlightplan(pilot, []byte("$AMN100:*A:N100:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n"))
	// ATC facility 0 ignored
	atc.FacilityType.Store(0)
	srv.handleAmendFlightplan(atc, []byte("$AMLAX_TWR:*A:N100:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n"))
	// ATC OK
	atc.FacilityType.Store(4)
	srv.handleAmendFlightplan(atc, []byte("$AMLAX_TWR:*A:N100:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n"))
	// missing target
	srv.handleAmendFlightplan(atc, []byte("$AMLAX_TWR:*A:NONE:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n"))
}

func TestVerifyPacket(t *testing.T) {
	s := newSess("N100", false, NetworkRatingObserver)

	// too short
	if _, ok := verifyPacket([]byte("a:b\r\n"), s); ok {
		t.Fatal("expected fail short")
	}
	// unknown type (login-like)
	if _, ok := verifyPacket([]byte("#APN100:SERVER:1:pass:1:1:1\r\n"), s); ok {
		t.Fatal("expected unknown for add pilot")
	}
	// source invalid
	if _, ok := verifyPacket([]byte("@OTHER:1:1200:1:34.0:-118.0:5000:250:0:0\r\n"), s); ok {
		t.Fatal("expected source invalid")
	}
	// good pilot pos: @MODE:CALLSIGN:...
	pt, ok := verifyPacket([]byte("@S:N100:1200:1:34.0:-118.0:5000:250:0:0\r\n"), s)
	if !ok || pt != PacketTypePilotPosition {
		t.Fatalf("verify pilot pos: pt=%v ok=%v outs=%v", pt, ok, drain(s))
	}
	// min fields fail — too few fields for pilot pos
	if _, ok := verifyPacket([]byte("@S:N100:1200\r\n"), s); ok {
		t.Fatal("expected min fields fail")
	}
	_ = drain(s)
}
