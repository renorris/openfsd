package server

import (
	"context"
	"io"
	"net"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeRegistry is an in-memory Registry for handler unit tests.
// Search/All deliver to every registered session except the source.
// There is no geo/range filter — intentional simplification for unit tests
// (handlers under test do not implement geo themselves).
//
// Search/All snapshot recipients under lock then invoke callbacks after unlock,
// matching postoffice deadlock semantics (callbacks may Session.Send / re-enter).
type fakeRegistry struct {
	mu       sync.Mutex
	sessions map[string]*session.Session

	// Recorded operations (for assertions)
	directSends []struct{ to, packet string }
	updates     []struct {
		callsign string
		center   [2]float64
		vis      float64
	}
	searchCalls int
	allCalls    int
}

// Compile-time interface satisfaction (matches deps.go production checks).
var _ Registry = (*fakeRegistry)(nil)

func newFakeRegistry(ss ...*session.Session) *fakeRegistry {
	r := &fakeRegistry{sessions: make(map[string]*session.Session)}
	for _, s := range ss {
		r.sessions[s.Callsign] = s
	}
	return r
}

func (r *fakeRegistry) Register(s *session.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[s.Callsign]; ok {
		return ErrCallsignInUse
	}
	r.sessions[s.Callsign] = s
	return nil
}

func (r *fakeRegistry) Release(s *session.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, s.Callsign)
}

func (r *fakeRegistry) UpdatePosition(s *session.Session, center [2]float64, visRangeM float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, struct {
		callsign string
		center   [2]float64
		vis      float64
	}{s.Callsign, center, visRangeM})
	s.SetLatLon(center[0], center[1])
	s.VisRange.Store(visRangeM)
}

func (r *fakeRegistry) Search(s *session.Session, fn func(*session.Session) bool) {
	r.mu.Lock()
	r.searchCalls++
	// Snapshot under lock; exclude self by pointer (matches postoffice.Search).
	// No geo filter — every other registered session is "in range."
	found := make([]*session.Session, 0, len(r.sessions))
	for _, other := range r.sessions {
		if other == s {
			continue
		}
		found = append(found, other)
	}
	r.mu.Unlock()

	for _, recipient := range found {
		if !fn(recipient) {
			return
		}
	}
}

func (r *fakeRegistry) All(except *session.Session, fn func(*session.Session) bool) {
	r.mu.Lock()
	r.allCalls++
	// Snapshot under lock; exclude by pointer identity (matches postoffice.All).
	recipients := make([]*session.Session, 0, len(r.sessions))
	for _, other := range r.sessions {
		if other == except {
			continue
		}
		recipients = append(recipients, other)
	}
	r.mu.Unlock()

	for _, recipient := range recipients {
		if !fn(recipient) {
			return
		}
	}
}

func (r *fakeRegistry) Send(callsign, packet string) error {
	r.mu.Lock()
	r.directSends = append(r.directSends, struct{ to, packet string }{callsign, packet})
	s, ok := r.sessions[callsign]
	r.mu.Unlock()
	if !ok {
		return ErrCallsignDoesNotExist
	}
	_ = s.Send(packet)
	return nil
}

func (r *fakeRegistry) Find(callsign string) (*session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[callsign]
	if !ok {
		return nil, ErrCallsignDoesNotExist
	}
	return s, nil
}

func (r *fakeRegistry) Snapshot() []*session.Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*session.Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out
}

// recordingMetar captures METAR requests.
type recordingMetar struct {
	mu       sync.Mutex
	requests []string
}

func (m *recordingMetar) Request(ctx context.Context, s session.Sender, icao string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, icao)
}

func (m *recordingMetar) Run(ctx context.Context) {}

func (m *recordingMetar) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return ""
	}
	return m.requests[len(m.requests)-1]
}

// fakeAuth implements session.Auth for auth-challenge tests.
type fakeAuth struct {
	resp    [32]byte
	updated bool
}

func newFakeAuth() *fakeAuth {
	a := &fakeAuth{}
	copy(a.resp[:], []byte("0123456789abcdef0123456789abcdef"))
	return a
}

func (a *fakeAuth) Initialize(clientID uint16, initialChallenge []byte) error { return nil }
func (a *fakeAuth) IsInitialized() bool                                       { return true }
func (a *fakeAuth) GetResponseForChallenge(challenge []byte) [32]byte         { return a.resp }
func (a *fakeAuth) UpdateState(d *[32]byte)                                   { a.updated = true }

// stubNetAddr / stubConn for IP query tests.
type stubNetAddr struct{ s string }

func (a stubNetAddr) Network() string { return "tcp" }
func (a stubNetAddr) String() string  { return a.s }

type stubConn struct {
	remote string
}

func (c *stubConn) Read(b []byte) (int, error)         { return 0, io.EOF }
func (c *stubConn) Write(b []byte) (int, error)        { return len(b), nil }
func (c *stubConn) Close() error                       { return nil }
func (c *stubConn) LocalAddr() net.Addr                { return stubNetAddr{"127.0.0.1:1"} }
func (c *stubConn) RemoteAddr() net.Addr               { return stubNetAddr{c.remote} }
func (c *stubConn) SetDeadline(t time.Time) error      { return nil }
func (c *stubConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *stubConn) SetWriteDeadline(t time.Time) error { return nil }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testServer(reg Registry, metar MetarQueue) *Server {
	d := fullDeps()
	if reg != nil {
		d.Registry = reg
	}
	if metar != nil {
		d.Metar = metar
	}
	s, err := New(d)
	if err != nil {
		panic(err)
	}
	return s
}

func newPilot(callsign string) *session.Session {
	return session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      callsign,
		NetworkRating: protocol.NetworkRatingObserver,
		ProtoRevision: 100,
		IsAtc:         false,
	})
}

func newATC(callsign string, facility int, rating protocol.NetworkRating) *session.Session {
	s := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      callsign,
		NetworkRating: rating,
		ProtoRevision: 100,
		IsAtc:         true,
	})
	s.FacilityType = facility
	return s
}

func newSUP(callsign string) *session.Session {
	return session.New(context.Background(), nil, nil, session.LoginData{
		Callsign:      callsign,
		NetworkRating: protocol.NetworkRatingSupervisor,
		ProtoRevision: 100,
		IsAtc:         true,
	})
}

func drainOutbound(s *session.Session) []string {
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

func hasErrorCode(packets []string, code int) bool {
	// FormatError: $ERserver:unknown:CODE::message\r\n
	for _, p := range packets {
		if !strings.HasPrefix(p, "$ER") {
			continue
		}
		fields := strings.Split(p, ":")
		if len(fields) >= 3 {
			var n int
			for _, c := range fields[2] {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			if n == code {
				return true
			}
		}
	}
	return false
}

func containsSubstring(packets []string, sub string) bool {
	for _, p := range packets {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

func funcName(fn handlerFunc) string {
	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

// ---------------------------------------------------------------------------
// getHandler dispatch
// ---------------------------------------------------------------------------

func TestGetHandlerDispatch(t *testing.T) {
	s := testServer(newFakeRegistry(), nil)

	tests := []struct {
		name string
		pt   PacketType
		want string // substring of function name
	}{
		{"text", PacketTypeTextMessage, "handleTextMessage"},
		{"atc_pos", PacketTypeATCPosition, "handleATCPosition"},
		{"pilot_pos", PacketTypePilotPosition, "handlePilotPosition"},
		{"fast", PacketTypePilotPositionFast, "handleFastPilotPosition"},
		{"slow", PacketTypePilotPositionSlow, "handleFastPilotPosition"},
		{"stopped", PacketTypePilotPositionStopped, "handleFastPilotPosition"},
		{"del_pilot", PacketTypeDeletePilot, "handleDelete"},
		{"del_atc", PacketTypeDeleteATC, "handleDelete"},
		{"sb", PacketTypeSquawkbox, "handleSquawkbox"},
		{"pc", PacketTypeProController, "handleProcontroller"},
		{"cq", PacketTypeClientQuery, "handleClientQuery"},
		{"cr", PacketTypeClientQueryResponse, "handleClientQuery"},
		{"kill", PacketTypeKillRequest, "handleKillRequest"},
		{"auth", PacketTypeAuthChallenge, "handleAuthChallenge"},
		{"ho", PacketTypeHandoffRequest, "handleHandoff"},
		{"ha", PacketTypeHandoffAccept, "handleHandoff"},
		{"metar", PacketTypeMetarRequest, "handleMetarRequest"},
		{"fp", PacketTypeFlightPlan, "handleFileFlightplan"},
		{"am", PacketTypeFlightPlanAmendment, "handleAmendFlightplan"},
		{"unknown", PacketTypeUnknown, "emptyHandler"},
		{"default", PacketType(999), "emptyHandler"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := s.getHandler(tc.pt)
			name := funcName(h)
			if !strings.Contains(name, tc.want) {
				t.Fatalf("getHandler(%v) = %s, want containing %q", tc.pt, name, tc.want)
			}
		})
	}
}

func TestEmptyHandler(t *testing.T) {
	s := testServer(newFakeRegistry(), nil)
	// Must not panic.
	s.emptyHandler(newPilot("X"), []byte("noop"))
}

// ---------------------------------------------------------------------------
// Text message
// ---------------------------------------------------------------------------

func TestHandleTextMessage(t *testing.T) {
	tests := []struct {
		name       string
		client     func() *session.Session
		packet     string
		setup      func(reg *fakeRegistry, client *session.Session) *session.Session // optional peer
		wantDirect string                                                            // if non-empty, expect direct send to this callsign
		wantSearch bool
		wantAll    bool
		wantErr    int // error code on client, 0 = none
		wantNoOp   bool
	}{
		{
			name:   "ATC chat as ATC",
			client: func() *session.Session { return newATC("EWR_TWR", 4, NetworkRatingStudent2) },
			packet: "#TMEWR_TWR:@49999:hello atc\r\n",
			setup: func(reg *fakeRegistry, client *session.Session) *session.Session {
				peer := newATC("JFK_GND", 3, NetworkRatingStudent1)
				_ = reg.Register(peer)
				return peer
			},
			wantSearch: true,
		},
		{
			name:     "ATC chat as pilot ignored",
			client:   func() *session.Session { return newPilot("N123AB") },
			packet:   "#TMN123AB:@49999:nope\r\n",
			wantNoOp: true,
		},
		{
			name:   "frequency broadcast",
			client: func() *session.Session { return newPilot("N123AB") },
			packet: "#TMN123AB:@22800:on freq\r\n",
			setup: func(reg *fakeRegistry, client *session.Session) *session.Session {
				peer := newPilot("N456CD")
				_ = reg.Register(peer)
				return peer
			},
			wantSearch: true,
		},
		{
			name:   "wallop to supervisors",
			client: func() *session.Session { return newPilot("N123AB") },
			packet: "#TMN123AB:*S:help\r\n",
			setup: func(reg *fakeRegistry, client *session.Session) *session.Session {
				sup := newSUP("SUP1")
				_ = reg.Register(sup)
				return sup
			},
			wantAll: true,
		},
		{
			name:   "server-wide broadcast as supervisor",
			client: func() *session.Session { return newSUP("SUP1") },
			packet: "#TMSUP1:*:broadcast\r\n",
			setup: func(reg *fakeRegistry, client *session.Session) *session.Session {
				peer := newPilot("N123AB")
				_ = reg.Register(peer)
				return peer
			},
			wantAll: true,
		},
		{
			name:     "server-wide broadcast denied for pilot",
			client:   func() *session.Session { return newPilot("N123AB") },
			packet:   "#TMN123AB:*:nope\r\n",
			wantNoOp: true,
		},
		{
			name:     "FP recipient is no-op",
			client:   func() *session.Session { return newPilot("N123AB") },
			packet:   "#TMN123AB:FP:ignored\r\n",
			wantNoOp: true,
		},
		{
			name:     "SERVER recipient is no-op",
			client:   func() *session.Session { return newPilot("N123AB") },
			packet:   "#TMN123AB:SERVER:ignored\r\n",
			wantNoOp: true,
		},
		{
			name:   "direct message ok",
			client: func() *session.Session { return newPilot("N123AB") },
			packet: "#TMN123AB:N456CD:private\r\n",
			setup: func(reg *fakeRegistry, client *session.Session) *session.Session {
				peer := newPilot("N456CD")
				_ = reg.Register(peer)
				return peer
			},
			wantDirect: "N456CD",
		},
		{
			name:    "direct message missing callsign",
			client:  func() *session.Session { return newPilot("N123AB") },
			packet:  "#TMN123AB:GHOST:private\r\n",
			wantErr: NoSuchCallsignError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.client()
			reg := newFakeRegistry(client)
			var peer *session.Session
			if tc.setup != nil {
				peer = tc.setup(reg, client)
			}
			s := testServer(reg, nil)
			s.handleTextMessage(client, []byte(tc.packet))

			if tc.wantDirect != "" {
				if len(reg.directSends) == 0 || reg.directSends[0].to != tc.wantDirect {
					t.Fatalf("directSends = %+v, want to %q", reg.directSends, tc.wantDirect)
				}
			}
			if tc.wantSearch && reg.searchCalls == 0 {
				t.Fatal("expected Search call")
			}
			if tc.wantAll && reg.allCalls == 0 {
				t.Fatal("expected All call")
			}
			if tc.wantErr != 0 {
				if !hasErrorCode(drainOutbound(client), tc.wantErr) {
					t.Fatalf("expected error code %d on client", tc.wantErr)
				}
			}
			if tc.wantNoOp {
				if len(reg.directSends) != 0 || reg.searchCalls != 0 || reg.allCalls != 0 {
					t.Fatalf("expected no-op, got sends=%v search=%d all=%d",
						reg.directSends, reg.searchCalls, reg.allCalls)
				}
			}
			_ = peer
		})
	}
}

// ---------------------------------------------------------------------------
// Position
// ---------------------------------------------------------------------------

func TestHandleATCPosition(t *testing.T) {
	tests := []struct {
		name       string
		rating     protocol.NetworkRating
		packet     string
		wantFac    int
		wantFreq   string
		wantErr    int
		wantCancel bool
		wantUpdate bool
	}{
		{
			name:       "valid tower position",
			rating:     NetworkRatingStudent2,
			packet:     "%EWR_TWR:28550:4:50:4:40.67:-74.18:0\r\n",
			wantFac:    4,
			wantFreq:   "28550",
			wantUpdate: true,
		},
		{
			name:    "invalid facility type syntax",
			rating:  NetworkRatingController1,
			packet:  "%EWR_TWR:28550:XX:50:5:40.67:-74.18:0\r\n",
			wantErr: SyntaxError,
		},
		{
			name:       "facility above rating cancels",
			rating:     NetworkRatingObserver,
			packet:     "%OBS:28550:4:50:1:40.67:-74.18:0\r\n",
			wantErr:    InvalidPositionForRatingError,
			wantCancel: true,
		},
		{
			name:    "invalid lat/lon",
			rating:  NetworkRatingStudent2,
			packet:  "%EWR_TWR:28550:4:50:4:bad:-74.18:0\r\n",
			wantErr: SyntaxError,
		},
		{
			name:    "invalid vis range",
			rating:  NetworkRatingStudent2,
			packet:  "%EWR_TWR:28550:4:xx:4:40.67:-74.18:0\r\n",
			wantErr: SyntaxError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newATC("EWR_TWR", 0, tc.rating)
			// Fix callsign to match packet source for OBS case
			if strings.Contains(tc.packet, "%OBS:") {
				client = newATC("OBS", 0, tc.rating)
			}
			reg := newFakeRegistry(client)
			s := testServer(reg, nil)
			s.handleATCPosition(client, []byte(tc.packet))

			if tc.wantUpdate {
				if len(reg.updates) != 1 {
					t.Fatalf("updates = %d, want 1", len(reg.updates))
				}
				if client.FacilityType != tc.wantFac {
					t.Fatalf("FacilityType = %d, want %d", client.FacilityType, tc.wantFac)
				}
				if client.Frequency.Load() != tc.wantFreq {
					t.Fatalf("Frequency = %q, want %q", client.Frequency.Load(), tc.wantFreq)
				}
				if reg.updates[0].center[0] != 40.67 {
					t.Fatalf("lat = %v, want 40.67", reg.updates[0].center[0])
				}
			}
			if tc.wantErr != 0 {
				if !hasErrorCode(drainOutbound(client), tc.wantErr) {
					t.Fatalf("expected error %d", tc.wantErr)
				}
			}
			if tc.wantCancel {
				select {
				case <-client.Ctx.Done():
				default:
					t.Fatal("expected client cancelled")
				}
			}
		})
	}
}

func TestHandlePilotPosition(t *testing.T) {
	// @S:CALLSIGN:SQUAWK:RATING:LAT:LON:ALT:GS:PBH:CORR
	const base = "@S:N123AB:1200:1:40.65:-73.79:3000:250:4261294148:0\r\n"

	t.Run("updates state and position", func(t *testing.T) {
		client := newPilot("N123AB")
		peer := newPilot("N456CD")
		reg := newFakeRegistry(client, peer)
		s := testServer(reg, nil)
		s.handlePilotPosition(client, []byte(base))

		if len(reg.updates) != 1 {
			t.Fatalf("updates = %d", len(reg.updates))
		}
		if reg.updates[0].vis != 50.0*1852.0 {
			t.Fatalf("vis = %v", reg.updates[0].vis)
		}
		if client.Transponder.Load() != "1200" {
			t.Fatalf("xpdr = %q", client.Transponder.Load())
		}
		if client.Groundspeed.Load() != 250 {
			t.Fatalf("gs = %d", client.Groundspeed.Load())
		}
		if client.Altitude.Load() != 3000 {
			t.Fatalf("alt = %d", client.Altitude.Load())
		}
		if client.Heading.Load() == 0 {
			// heading from fixture is ~5.97; non-zero is enough
			t.Fatalf("heading unexpectedly 0")
		}
		// peer should receive broadcast
		if len(drainOutbound(peer)) == 0 {
			t.Fatal("peer should receive ranged broadcast")
		}
	})

	t.Run("invalid lat/lon", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handlePilotPosition(client, []byte("@S:N123AB:1200:1:xx:-73.79:3000:250:1:0\r\n"))
		if !hasErrorCode(drainOutbound(client), SyntaxError) {
			t.Fatal("expected syntax error")
		}
	})

	t.Run("sendfast enable when close", func(t *testing.T) {
		client := newPilot("N123AB")
		client.ProtoRevision = 101
		client.SendFastEnabled = false
		client.ClosestVelocityClientDistance = 2.0 * 1852.0 // 2 NM
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handlePilotPosition(client, []byte(base))
		if !client.SendFastEnabled {
			t.Fatal("expected SendFastEnabled true")
		}
		out := drainOutbound(client)
		if !containsSubstring(out, "$SFSERVER:N123AB:1") {
			t.Fatalf("expected enable SF packet, got %v", out)
		}
	})

	t.Run("sendfast disable when far", func(t *testing.T) {
		client := newPilot("N123AB")
		client.ProtoRevision = 101
		client.SendFastEnabled = true
		client.ClosestVelocityClientDistance = 10.0 * 1852.0 // 10 NM
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handlePilotPosition(client, []byte(base))
		if client.SendFastEnabled {
			t.Fatal("expected SendFastEnabled false")
		}
		out := drainOutbound(client)
		if !containsSubstring(out, "$SFSERVER:N123AB:0") {
			t.Fatalf("expected disable SF packet, got %v", out)
		}
	})
}

func TestHandleFastPilotPosition(t *testing.T) {
	src := newPilot("DAL1151")
	src.ProtoRevision = 101
	peer101 := newPilot("UAL100")
	peer101.ProtoRevision = 101
	peer100 := newPilot("AAL200")
	peer100.ProtoRevision = 100

	reg := newFakeRegistry(src, peer101, peer100)
	s := testServer(reg, nil)
	pkt := []byte("^DAL1151:40.6:-73.7:16:8:1:0:0:0:0:0:0:0\r\n")
	s.handleFastPilotPosition(src, pkt)

	if reg.searchCalls != 1 {
		t.Fatalf("searchCalls = %d", reg.searchCalls)
	}
	if len(drainOutbound(peer101)) == 0 {
		t.Fatal("proto 101 peer should receive velocity packet")
	}
	if len(drainOutbound(peer100)) != 0 {
		t.Fatal("proto 100 peer should not receive velocity packet")
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestHandleDelete(t *testing.T) {
	client := newPilot("N123AB")
	peer := newPilot("N456CD")
	reg := newFakeRegistry(client, peer)
	s := testServer(reg, nil)
	s.handleDelete(client, []byte("#DPN123AB:SERVER\r\n"))

	if reg.allCalls != 1 {
		t.Fatalf("allCalls = %d", reg.allCalls)
	}
	if len(drainOutbound(peer)) == 0 {
		t.Fatal("peer should receive delete broadcast")
	}
	select {
	case <-client.Ctx.Done():
	default:
		t.Fatal("client should be cancelled")
	}
}

// ---------------------------------------------------------------------------
// Squawkbox / ProController
// ---------------------------------------------------------------------------

func TestHandleSquawkbox(t *testing.T) {
	src := newPilot("N123AB")
	dst := newPilot("N456CD")
	reg := newFakeRegistry(src, dst)
	s := testServer(reg, nil)
	s.handleSquawkbox(src, []byte("#SBN123AB:N456CD:P2\r\n"))
	if len(reg.directSends) != 1 || reg.directSends[0].to != "N456CD" {
		t.Fatalf("directSends = %+v", reg.directSends)
	}

	// missing recipient
	s.handleSquawkbox(src, []byte("#SBN123AB:GHOST:P2\r\n"))
	if !hasErrorCode(drainOutbound(src), NoSuchCallsignError) {
		t.Fatal("expected no such callsign")
	}
}

func TestHandleProcontroller(t *testing.T) {
	tests := []struct {
		name       string
		client     func() *session.Session
		packet     string
		wantDirect string
		wantSearch bool
		wantErr    int
		wantNoOp   bool
	}{
		{
			name:     "pilot ignored",
			client:   func() *session.Session { return newPilot("N123") },
			packet:   "#PCN123:EWR_TWR:CCP:VER\r\n",
			wantNoOp: true,
		},
		{
			name:    "invalid short recipient",
			client:  func() *session.Session { return newATC("EWR_TWR", 4, NetworkRatingStudent2) },
			packet:  "#PCEWR_TWR:X:CCP:VER\r\n",
			wantErr: SyntaxError,
		},
		{
			name: "unprivileged VER forward",
			client: func() *session.Session {
				return newATC("EWR_TWR", 0, NetworkRatingStudent2) // OBS facility OK for unprivileged
			},
			packet:     "#PCEWR_TWR:JFK_GND:CCP:VER\r\n",
			wantDirect: "JFK_GND",
		},
		{
			name: "privileged BC denied for OBS facility",
			client: func() *session.Session {
				return newATC("EWR_TWR", 0, NetworkRatingStudent2)
			},
			packet:  "#PCEWR_TWR:JFK_GND:CCP:BC:N123:1200\r\n",
			wantErr: InvalidControlError,
		},
		{
			name: "privileged BC direct",
			client: func() *session.Session {
				return newATC("EWR_TWR", 4, NetworkRatingStudent2)
			},
			packet:     "#PCEWR_TWR:JFK_GND:CCP:BC:N123:1200\r\n",
			wantDirect: "JFK_GND",
		},
		{
			name: "privileged BC freq broadcast",
			client: func() *session.Session {
				return newATC("EWR_TWR", 4, NetworkRatingStudent2)
			},
			packet:     "#PCEWR_TWR:@22800:CCP:BC:N123:1200\r\n",
			wantSearch: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.client()
			peer := newATC("JFK_GND", 3, NetworkRatingStudent1)
			reg := newFakeRegistry(client, peer)
			s := testServer(reg, nil)
			s.handleProcontroller(client, []byte(tc.packet))

			if tc.wantNoOp {
				if len(reg.directSends) != 0 || reg.searchCalls != 0 {
					t.Fatalf("expected no-op")
				}
				return
			}
			if tc.wantDirect != "" {
				if len(reg.directSends) == 0 || reg.directSends[0].to != tc.wantDirect {
					t.Fatalf("directSends = %+v", reg.directSends)
				}
			}
			if tc.wantSearch && reg.searchCalls == 0 {
				t.Fatal("expected Search")
			}
			if tc.wantErr != 0 && !hasErrorCode(drainOutbound(client), tc.wantErr) {
				t.Fatalf("expected error %d", tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Client query
// ---------------------------------------------------------------------------

func TestHandleClientQuery(t *testing.T) {
	t.Run("SERVER ATC yes", func(t *testing.T) {
		client := newPilot("N123AB")
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		reg := newFakeRegistry(client, atc)
		s := testServer(reg, nil)
		s.handleClientQuery(client, []byte("$CQN123AB:SERVER:ATC:EWR_TWR\r\n"))
		out := drainOutbound(client)
		if !containsSubstring(out, "ATC:Y:EWR_TWR") {
			t.Fatalf("got %v", out)
		}
	})

	t.Run("SERVER ATC no for OBS", func(t *testing.T) {
		client := newPilot("N123AB")
		obs := newATC("OBS", 0, NetworkRatingObserver)
		reg := newFakeRegistry(client, obs)
		s := testServer(reg, nil)
		s.handleClientQuery(client, []byte("$CQN123AB:SERVER:ATC:OBS\r\n"))
		out := drainOutbound(client)
		if !containsSubstring(out, "ATC:N:OBS") {
			t.Fatalf("got %v", out)
		}
	})

	t.Run("SERVER ATC missing", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handleClientQuery(client, []byte("$CQN123AB:SERVER:ATC:GHOST\r\n"))
		if !hasErrorCode(drainOutbound(client), NoSuchCallsignError) {
			t.Fatal("expected no such callsign")
		}
	})

	t.Run("SERVER ATC bad field count", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handleClientQuery(client, []byte("$CQN123AB:SERVER:ATC\r\n"))
		if !hasErrorCode(drainOutbound(client), SyntaxError) {
			t.Fatal("expected syntax error")
		}
	})

	t.Run("SERVER IP", func(t *testing.T) {
		client := session.New(context.Background(), &stubConn{remote: "203.0.113.9:54321"}, nil, session.LoginData{
			Callsign: "N123AB",
		})
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handleClientQuery(client, []byte("$CQN123AB:SERVER:IP\r\n"))
		out := drainOutbound(client)
		if !containsSubstring(out, "IP:203.0.113.9") {
			t.Fatalf("got %v", out)
		}
	})

	t.Run("SERVER FP as ATC with plan", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		pilot := newPilot("N123AB")
		pilot.FlightPlan.Store("V:N123AB:1200:KSFO:FL350:KJFK")
		pilot.AssignedBeaconCode.Store("1200")
		reg := newFakeRegistry(atc, pilot)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:SERVER:FP:N123AB\r\n"))
		out := drainOutbound(atc)
		if !containsSubstring(out, "$FPN123AB:*A:") {
			t.Fatalf("missing FP packet: %v", out)
		}
		if !containsSubstring(out, "#PCserver:EWR_TWR:CCP:BC:N123AB:1200") {
			t.Fatalf("missing BC packet: %v", out)
		}
	})

	t.Run("SERVER FP as pilot ignored", func(t *testing.T) {
		pilot := newPilot("N123AB")
		reg := newFakeRegistry(pilot)
		s := testServer(reg, nil)
		s.handleClientQuery(pilot, []byte("$CQN123AB:SERVER:FP:N123AB\r\n"))
		if len(drainOutbound(pilot)) != 0 {
			t.Fatal("pilot should not get FP response")
		}
	})

	t.Run("SERVER FP missing target", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		reg := newFakeRegistry(atc)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:SERVER:FP:GHOST\r\n"))
		if !hasErrorCode(drainOutbound(atc), NoSuchCallsignError) {
			t.Fatal("expected no such callsign")
		}
	})

	t.Run("SERVER FP no plan stored", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		pilot := newPilot("N123AB")
		reg := newFakeRegistry(atc, pilot)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:SERVER:FP:N123AB\r\n"))
		if len(drainOutbound(atc)) != 0 {
			t.Fatal("empty FP should send nothing")
		}
	})

	t.Run("SERVER FP default beacon 0", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		pilot := newPilot("N123AB")
		pilot.FlightPlan.Store("V:N123AB:0:KSFO:FL350:KJFK")
		reg := newFakeRegistry(atc, pilot)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:SERVER:FP:N123AB\r\n"))
		out := drainOutbound(atc)
		if !containsSubstring(out, ":0\r\n") {
			t.Fatalf("expected default beacon 0: %v", out)
		}
	})

	t.Run("SERVER FP bad field count", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		reg := newFakeRegistry(atc)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:SERVER:FP\r\n"))
		if !hasErrorCode(drainOutbound(atc), SyntaxError) {
			t.Fatal("expected syntax error")
		}
	})

	t.Run("unprivileged ATC query BY", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		peer := newATC("JFK_GND", 3, NetworkRatingStudent1)
		reg := newFakeRegistry(atc, peer)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:JFK_GND:BY\r\n"))
		if len(reg.directSends) != 1 {
			t.Fatalf("directSends = %+v", reg.directSends)
		}
	})

	t.Run("unprivileged ATC query denied for pilot", func(t *testing.T) {
		pilot := newPilot("N123AB")
		reg := newFakeRegistry(pilot)
		s := testServer(reg, nil)
		s.handleClientQuery(pilot, []byte("$CQN123AB:EWR_TWR:BY\r\n"))
		if !hasErrorCode(drainOutbound(pilot), InvalidControlError) {
			t.Fatal("expected invalid control")
		}
	})

	t.Run("privileged ATC query IT", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		peer := newATC("JFK_GND", 3, NetworkRatingStudent1)
		reg := newFakeRegistry(atc, peer)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:JFK_GND:IT:N123\r\n"))
		if len(reg.directSends) != 1 {
			t.Fatalf("directSends = %+v", reg.directSends)
		}
	})

	t.Run("privileged ATC query denied for OBS", func(t *testing.T) {
		obs := newATC("OBS", 0, NetworkRatingObserver)
		reg := newFakeRegistry(obs)
		s := testServer(reg, nil)
		s.handleClientQuery(obs, []byte("$CQOBS:EWR_TWR:IT:N123\r\n"))
		if !hasErrorCode(drainOutbound(obs), InvalidControlError) {
			t.Fatal("expected invalid control")
		}
	})

	t.Run("aircraft config CAPS any client", func(t *testing.T) {
		pilot := newPilot("N123AB")
		dst := newPilot("N456CD")
		reg := newFakeRegistry(pilot, dst)
		s := testServer(reg, nil)
		s.handleClientQuery(pilot, []byte("$CQN123AB:N456CD:CAPS\r\n"))
		if len(reg.directSends) != 1 {
			t.Fatalf("directSends = %+v", reg.directSends)
		}
	})

	t.Run("INF interrogation requires SUP", func(t *testing.T) {
		pilot := newPilot("N123AB")
		reg := newFakeRegistry(pilot)
		s := testServer(reg, nil)
		s.handleClientQuery(pilot, []byte("$CQN123AB:N456CD:INF\r\n"))
		if !hasErrorCode(drainOutbound(pilot), InvalidControlError) {
			t.Fatal("expected invalid control")
		}
	})

	t.Run("INF interrogation as SUP", func(t *testing.T) {
		sup := newSUP("SUP1")
		dst := newPilot("N456CD")
		reg := newFakeRegistry(sup, dst)
		s := testServer(reg, nil)
		s.handleClientQuery(sup, []byte("$CQSUP1:N456CD:INF\r\n"))
		if len(reg.directSends) != 1 {
			t.Fatalf("directSends = %+v", reg.directSends)
		}
	})

	t.Run("INF response allowed", func(t *testing.T) {
		pilot := newPilot("N123AB")
		sup := newSUP("SUP1")
		reg := newFakeRegistry(pilot, sup)
		s := testServer(reg, nil)
		s.handleClientQuery(pilot, []byte("$CRN123AB:SUP1:INF:info\r\n"))
		if len(reg.directSends) != 1 || reg.directSends[0].to != "SUP1" {
			t.Fatalf("directSends = %+v", reg.directSends)
		}
	})

	t.Run("forward invalid recipient", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		reg := newFakeRegistry(atc)
		s := testServer(reg, nil)
		// recipient "X" has len 1 -> invalid
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:X:BY\r\n"))
		if !hasErrorCode(drainOutbound(atc), NoSuchCallsignError) {
			t.Fatal("expected invalid recipient error")
		}
	})

	t.Run("forward ranged ATC special recipient", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		peer := newATC("JFK_GND", 3, NetworkRatingStudent1)
		reg := newFakeRegistry(atc, peer)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:@94835:NEWATIS:A\r\n"))
		if reg.searchCalls != 1 {
			t.Fatalf("searchCalls = %d", reg.searchCalls)
		}
	})

	t.Run("forward ranged all special recipient", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		peer := newPilot("N123AB")
		reg := newFakeRegistry(atc, peer)
		s := testServer(reg, nil)
		s.handleClientQuery(atc, []byte("$CQEWR_TWR:@94836:NEWINFO:foo\r\n"))
		if reg.searchCalls != 1 {
			t.Fatalf("searchCalls = %d", reg.searchCalls)
		}
	})
}

// ---------------------------------------------------------------------------
// Kill (admin)
// ---------------------------------------------------------------------------

func TestHandleKillRequest(t *testing.T) {
	tests := []struct {
		name       string
		client     func() *session.Session
		packet     string
		victimCS   string // if non-empty, register this callsign as victim
		wantCancel bool
		wantErr    int
		wantNoOp   bool
	}{
		{
			name:       "supervisor kills victim",
			client:     func() *session.Session { return newSUP("SUP1") },
			packet:     "$!!SUP1:N123AB:reason\r\n",
			victimCS:   "N123AB",
			wantCancel: true,
		},
		{
			// Non-sup must not cancel a registered victim and must not emit $ER
			// (rating gate returns before Find). Without a victim this subtest
			// would assert nothing if the gate were dropped and Find failed.
			name:     "non-sup ignored",
			client:   func() *session.Session { return newPilot("N123AB") },
			packet:   "$!!N123AB:N456CD:reason\r\n",
			victimCS: "N456CD",
			wantNoOp: true,
		},
		{
			name:    "missing victim",
			client:  func() *session.Session { return newSUP("SUP1") },
			packet:  "$!!SUP1:GHOST:reason\r\n",
			wantErr: NoSuchCallsignError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.client()
			var victim *session.Session
			reg := newFakeRegistry(client)
			if tc.victimCS != "" {
				victim = newPilot(tc.victimCS)
				_ = reg.Register(victim)
			}
			s := testServer(reg, nil)
			s.handleKillRequest(client, []byte(tc.packet))

			if tc.wantCancel {
				select {
				case <-victim.Ctx.Done():
				default:
					t.Fatal("victim should be cancelled")
				}
			}
			out := drainOutbound(client)
			if tc.wantErr != 0 && !hasErrorCode(out, tc.wantErr) {
				t.Fatalf("expected error %d, packets=%v", tc.wantErr, out)
			}
			if tc.wantNoOp {
				if victim == nil {
					t.Fatal("wantNoOp requires a registered victim")
				}
				select {
				case <-victim.Ctx.Done():
					t.Fatal("victim should not be cancelled")
				default:
				}
				// Rating gate returns before Find: no $ER and no kill side effects.
				if len(out) != 0 {
					t.Fatalf("non-sup should emit no packets, got %v", out)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Auth / handoff
// ---------------------------------------------------------------------------

func TestHandleAuthChallenge(t *testing.T) {
	t.Run("no initial challenge", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handleAuthChallenge(client, []byte("$ZCN123AB:SERVER:abcdef\r\n"))
		if !hasErrorCode(drainOutbound(client), UnauthorizedSoftwareError) {
			t.Fatal("expected unauthorized software error")
		}
	})

	t.Run("success", func(t *testing.T) {
		client := newPilot("N123AB")
		client.ClientChallenge = "init"
		auth := newFakeAuth()
		client.Auth = auth
		reg := newFakeRegistry(client)
		s := testServer(reg, nil)
		s.handleAuthChallenge(client, []byte("$ZCN123AB:SERVER:abcdef12\r\n"))
		out := drainOutbound(client)
		if !containsSubstring(out, "$ZRSERVER:N123AB:") {
			t.Fatalf("got %v", out)
		}
		if !auth.updated {
			t.Fatal("expected UpdateState")
		}
	})
}

func TestHandleHandoff(t *testing.T) {
	tests := []struct {
		name       string
		facility   int
		isAtc      bool
		wantDirect bool
		wantNoOp   bool
	}{
		{name: "active tower", facility: 4, isAtc: true, wantDirect: true},
		{name: "OBS facility denied", facility: 0, isAtc: true, wantNoOp: true},
		{name: "DEL facility denied (fac<=1)", facility: 1, isAtc: true, wantNoOp: true},
		{name: "pilot denied", facility: 4, isAtc: false, wantNoOp: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var client *session.Session
			if tc.isAtc {
				client = newATC("EWR_TWR", tc.facility, NetworkRatingStudent2)
			} else {
				client = newPilot("N123AB")
			}
			dst := newATC("JFK_APP", 5, NetworkRatingStudent3)
			reg := newFakeRegistry(client, dst)
			s := testServer(reg, nil)
			pkt := []byte("$HO" + client.Callsign + ":JFK_APP:N999\r\n")
			s.handleHandoff(client, pkt)

			if tc.wantDirect && (len(reg.directSends) == 0 || reg.directSends[0].to != "JFK_APP") {
				t.Fatalf("directSends = %+v", reg.directSends)
			}
			if tc.wantNoOp && len(reg.directSends) != 0 {
				t.Fatalf("expected no-op, got %+v", reg.directSends)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// METAR
// ---------------------------------------------------------------------------

func TestHandleMetarRequest(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		metar := &recordingMetar{}
		s := testServer(reg, metar)
		s.handleMetarRequest(client, []byte("$AXN123AB:SERVER:METAR:KJFK\r\n"))
		if metar.last() != "KJFK" {
			t.Fatalf("metar = %q", metar.last())
		}
	})

	t.Run("wrong recipient", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		metar := &recordingMetar{}
		s := testServer(reg, metar)
		s.handleMetarRequest(client, []byte("$AXN123AB:OTHER:METAR:KJFK\r\n"))
		if metar.last() != "" {
			t.Fatal("should not request metar")
		}
	})

	t.Run("wrong static field", func(t *testing.T) {
		client := newPilot("N123AB")
		reg := newFakeRegistry(client)
		metar := &recordingMetar{}
		s := testServer(reg, metar)
		s.handleMetarRequest(client, []byte("$AXN123AB:SERVER:TAF:KJFK\r\n"))
		if metar.last() != "" {
			t.Fatal("should not request metar")
		}
	})
}

// ---------------------------------------------------------------------------
// Flight plan
// ---------------------------------------------------------------------------

func TestHandleFileFlightplan(t *testing.T) {
	// Min fields is 17; pad with empty fields.
	// $FPCALLSIGN:DEST: + 15 info fields
	fplFields := make([]string, 15)
	for i := range fplFields {
		fplFields[i] = "X"
	}
	fplInfo := strings.Join(fplFields, ":")
	packet := "$FPN123AB:*A:" + fplInfo + "\r\n"

	pilot := newPilot("N123AB")
	atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
	otherPilot := newPilot("N456CD")
	reg := newFakeRegistry(pilot, atc, otherPilot)
	s := testServer(reg, nil)
	s.handleFileFlightplan(pilot, []byte(packet))

	if pilot.FlightPlan.Load() != fplInfo {
		t.Fatalf("FlightPlan = %q, want %q", pilot.FlightPlan.Load(), fplInfo)
	}
	atcOut := drainOutbound(atc)
	if !containsSubstring(atcOut, "$FPN123AB:*A:") {
		t.Fatalf("ATC should receive FP broadcast: %v", atcOut)
	}
	if len(drainOutbound(otherPilot)) != 0 {
		t.Fatal("non-ATC should not receive FP broadcast")
	}
}

func TestHandleAmendFlightplan(t *testing.T) {
	// $AMSOURCE:DEST:TARGET: + 15 info fields (min 18 fields total)
	fplFields := make([]string, 15)
	for i := range fplFields {
		fplFields[i] = "Y"
	}
	fplInfo := strings.Join(fplFields, ":")

	t.Run("success", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		pilot := newPilot("N123AB")
		peerATC := newATC("JFK_GND", 3, NetworkRatingStudent1)
		reg := newFakeRegistry(atc, pilot, peerATC)
		s := testServer(reg, nil)
		packet := "$AMEWR_TWR:*A:N123AB:" + fplInfo + "\r\n"
		s.handleAmendFlightplan(atc, []byte(packet))

		if pilot.FlightPlan.Load() != fplInfo {
			t.Fatalf("FlightPlan = %q", pilot.FlightPlan.Load())
		}
		if !containsSubstring(drainOutbound(peerATC), "$AMEWR_TWR:*A:N123AB:") {
			t.Fatal("peer ATC should receive AM broadcast")
		}
	})

	t.Run("denied for OBS", func(t *testing.T) {
		obs := newATC("OBS", 0, NetworkRatingObserver)
		pilot := newPilot("N123AB")
		reg := newFakeRegistry(obs, pilot)
		s := testServer(reg, nil)
		packet := "$AMOBS:*A:N123AB:" + fplInfo + "\r\n"
		s.handleAmendFlightplan(obs, []byte(packet))
		if pilot.FlightPlan.Load() != "" {
			t.Fatal("should not amend")
		}
	})

	t.Run("denied for pilot", func(t *testing.T) {
		pilot := newPilot("N999ZZ")
		target := newPilot("N123AB")
		reg := newFakeRegistry(pilot, target)
		s := testServer(reg, nil)
		packet := "$AMN999ZZ:*A:N123AB:" + fplInfo + "\r\n"
		s.handleAmendFlightplan(pilot, []byte(packet))
		if target.FlightPlan.Load() != "" {
			t.Fatal("should not amend")
		}
	})

	t.Run("missing target", func(t *testing.T) {
		atc := newATC("EWR_TWR", 4, NetworkRatingStudent2)
		reg := newFakeRegistry(atc)
		s := testServer(reg, nil)
		packet := "$AMEWR_TWR:*A:GHOST:" + fplInfo + "\r\n"
		s.handleAmendFlightplan(atc, []byte(packet))
		if !hasErrorCode(drainOutbound(atc), NoSuchCallsignError) {
			t.Fatal("expected no such callsign")
		}
	})
}

// ---------------------------------------------------------------------------
// Integration-ish: verifyPacket + getHandler path smoke
// ---------------------------------------------------------------------------

func TestVerifyPacketAndDispatchSmoke(t *testing.T) {
	client := newPilot("N123AB")
	peer := newPilot("N456CD")
	reg := newFakeRegistry(client, peer)
	s := testServer(reg, nil)

	packet := []byte("#TMN123AB:N456CD:hello\r\n")
	pt, ok := verifyPacket(packet, client)
	if !ok || pt != PacketTypeTextMessage {
		t.Fatalf("verifyPacket = (%v, %v)", pt, ok)
	}
	s.getHandler(pt)(client, packet)
	if len(reg.directSends) != 1 {
		t.Fatalf("directSends = %+v", reg.directSends)
	}
}

func TestVerifyPacketErrors(t *testing.T) {
	client := newPilot("N123AB")

	tests := []struct {
		name    string
		packet  string
		wantErr int
	}{
		{"too short", "ab", SyntaxError},
		{"unknown type", "$ZZabc:def:ghi\r\n", SyntaxError},
		{"source invalid", "#TMOTHER:N456CD:hi\r\n", SourceInvalidError},
		{"min fields", "#TMN123AB:X\r\n", SyntaxError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// fresh client each time so outbound is clean
			c := newPilot("N123AB")
			_, ok := verifyPacket([]byte(tc.packet), c)
			if ok {
				t.Fatal("expected not ok")
			}
			out := drainOutbound(c)
			if !hasErrorCode(out, tc.wantErr) {
				t.Fatalf("expected error %d, packets=%v", tc.wantErr, out)
			}
			_ = client
		})
	}
}
