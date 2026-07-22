package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

func TestMostLikelyJwt(t *testing.T) {
	// Minimal HS256-looking header.payload.sig (base64 std of {"alg":"HS256","typ":"JWT"})
	// {"alg":"HS256","typ":"JWT"} base64 = eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9
	good := []byte("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.sig")
	if !mostLikelyJwt(good) {
		t.Fatal("expected true for JWT-shaped token")
	}
	if mostLikelyJwt([]byte("not.a.jwt.extra")) {
		t.Fatal("too many dots")
	}
	if mostLikelyJwt([]byte("nodots")) {
		t.Fatal("no dots")
	}
	if mostLikelyJwt([]byte("a.b")) {
		t.Fatal("one dot only")
	}
	if mostLikelyJwt([]byte("!!!bad!!!.payload.sig")) {
		t.Fatal("bad header b64")
	}
	// valid b64 but not JWT header
	if mostLikelyJwt([]byte("e30.payload.sig")) { // {}
		t.Fatal("empty object header")
	}
}

func TestCallsignValidation(t *testing.T) {
	if isValidClientCallsign([]byte("A")) {
		t.Fatal("too short")
	}
	if isValidClientCallsign([]byte("ABCDEFGHIJK")) {
		t.Fatal("too long")
	}
	if isValidClientCallsign([]byte("ab")) {
		t.Fatal("lowercase")
	}
	if isValidClientCallsign([]byte("SERVER")) {
		t.Fatal("reserved")
	}
	if !isValidClientCallsign([]byte("N123AB")) {
		t.Fatal("expected valid")
	}
	if !isValidClientCallsign([]byte("LAX_TWR")) {
		t.Fatal("underscore ok")
	}
	if !isValidClientCallsign([]byte("N1-2")) {
		t.Fatal("hyphen ok")
	}
}

func TestIsAllowedFacilityType(t *testing.T) {
	if !isAllowedFacilityType(NetworkRatingObserver, 0) {
		t.Fatal("OBS facility always allowed")
	}
	if isAllowedFacilityType(NetworkRatingObserver, 4) {
		t.Fatal("OBS cannot do TWR")
	}
	if !isAllowedFacilityType(NetworkRatingStudent2, 4) {
		t.Fatal("S2 can do TWR")
	}
	if isAllowedFacilityType(NetworkRatingController1, 99) {
		t.Fatal("invalid facility")
	}
	if !isAllowedFacilityType(NetworkRatingController1, 6) {
		t.Fatal("C1 can do CTR")
	}
}

func TestParseLatLonVisRange(t *testing.T) {
	pkt := []byte("%CS:FREQ:2:40:1:33.5:-117.25:0\r\n")
	lat, lon, ok := parseLatLon(pkt, 5, 6)
	if !ok || lat != 33.5 || lon != -117.25 {
		t.Fatalf("parseLatLon = %v %v %v", lat, lon, ok)
	}
	if _, _, ok := parseLatLon([]byte("a:b:c"), 0, 1); ok {
		t.Fatal("expected fail")
	}
	vr, ok := parseVisRange(pkt, 3, 1500)
	if !ok || vr != 40*1852 {
		t.Fatalf("visRange = %v ok=%v", vr, ok)
	}
	if _, ok := parseVisRange([]byte("a:b:c:xx"), 3, 1500); ok {
		t.Fatal("expected vis fail")
	}
}

func TestBroadcastHelpers(t *testing.T) {
	reg := postoffice.New()
	a := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "A", IsAtc: true, NetworkRating: protocol.NetworkRatingSupervisor, ProtoRevision: 101,
	})
	b := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "B", IsAtc: false, NetworkRating: protocol.NetworkRatingObserver, ProtoRevision: 101,
	})
	c := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "C", IsAtc: true, NetworkRating: protocol.NetworkRatingController1, ProtoRevision: 100,
	})
	for _, s := range []*session.Session{a, b, c} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 100*1852)
	}

	broadcastRanged(reg, a, []byte("ranged\r\n"))
	broadcastRangedVelocity(reg, a, []byte("vel\r\n"))
	broadcastRangedAtcOnly(reg, a, []byte("atc\r\n"))
	broadcastAll(reg, a, []byte("all\r\n"))
	broadcastAllATC(reg, a, []byte("allatc\r\n"))
	broadcastAllSupervisors(reg, a, []byte("sup\r\n"))

	// direct ok / missing
	sendDirectOrErr(reg, a, []byte("B"), []byte("dm\r\n"))
	sendDirectOrErr(reg, a, []byte("NONE"), []byte("dm\r\n"))

	// forwardClientQuery branches
	forwardClientQuery(reg, a, []byte("$CQA:@94835:BY\r\n"))
	forwardClientQuery(reg, a, []byte("$CQA:@94836:BY\r\n"))
	forwardClientQuery(reg, a, []byte("$CQA:X:BY\r\n"))
	forwardClientQuery(reg, a, []byte("$CQA:B:BY\r\n"))

	_ = drain(a)
	_ = drain(b)
	_ = drain(c)
}

// TestBroadcastRanged_SlowPeerDoesNotStall is the UX contract for position storms:
// a recipient with a full outbound buffer must not block fan-out to healthy peers.
func TestBroadcastRanged_SlowPeerDoesNotStall(t *testing.T) {
	reg := postoffice.New()
	src := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "SRC", ProtoRevision: 101,
	})
	slow := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "SLOW", ProtoRevision: 101,
	})
	fast := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "FAST", ProtoRevision: 101,
	})
	for _, s := range []*session.Session{src, slow, fast} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 100*1852)
	}
	// Saturate slow peer (sendChanCap is 32).
	for i := 0; i < 32; i++ {
		if err := slow.Send("old"); err != nil {
			t.Fatal(err)
		}
	}

	pkt := []byte("@S:SRC:1200:1:34.0:-118.0:5000:250:0:0\r\n")
	done := make(chan struct{})
	go func() {
		broadcastRanged(reg, src, pkt)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("broadcastRanged blocked on slow peer — SendPosition contract broken")
	}

	fastOut := drain(fast)
	if !hasOutboundContaining(fastOut, "@S:SRC") {
		t.Fatalf("FAST peer missed position fan-out: %v", fastOut)
	}
	slowOut := drain(slow)
	if !hasOutboundContaining(slowOut, "@S:SRC") {
		t.Fatalf("SLOW peer should still get latest-wins position: %v", slowOut)
	}
}

// TestBroadcastHelpers_SkipSynthetic ensures every fan-out helper omits
// Synthetic recipients while still delivering to human peers.
// Direct registry.Send is intentionally not skipped (SenderWorker drain).
func TestBroadcastHelpers_SkipSynthetic(t *testing.T) {
	const marker = "SYNTHSKIP"
	pos := [2]float64{34, -118}
	vr := 100 * 1852.0

	// humanATC qualifies for ATC-only and supervisor fan-out.
	newHumanATC := func() *session.Session {
		return session.New(context.Background(), nil, nil, session.LoginData{
			Callsign:      "HUMAN",
			IsAtc:         true,
			NetworkRating: protocol.NetworkRatingSupervisor,
			ProtoRevision: 101,
		})
	}
	newSynth := func(cs string, isAtc bool, rating protocol.NetworkRating, proto int) *session.Session {
		s := session.New(context.Background(), nil, nil, session.LoginData{
			Callsign:      cs,
			IsAtc:         isAtc,
			NetworkRating: rating,
			ProtoRevision: proto,
		})
		s.Synthetic = true
		return s
	}

	type helperCase struct {
		name   string
		setup  func(t *testing.T) (reg *postoffice.PostOffice, src, human, synth *session.Session)
		invoke func(reg *postoffice.PostOffice, src *session.Session)
	}

	cases := []helperCase{
		{
			name: "broadcastRanged",
			setup: func(t *testing.T) (*postoffice.PostOffice, *session.Session, *session.Session, *session.Session) {
				reg := postoffice.New()
				src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC", ProtoRevision: 101})
				human := newHumanATC()
				synth := newSynth("SYN", false, protocol.NetworkRatingObserver, 101)
				for _, s := range []*session.Session{src, human, synth} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
					reg.UpdatePosition(s, pos, vr)
				}
				return reg, src, human, synth
			},
			invoke: func(reg *postoffice.PostOffice, src *session.Session) {
				broadcastRanged(reg, src, []byte(marker+"-ranged\r\n"))
			},
		},
		{
			name: "broadcastRangedVelocity",
			setup: func(t *testing.T) (*postoffice.PostOffice, *session.Session, *session.Session, *session.Session) {
				reg := postoffice.New()
				src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC", ProtoRevision: 101})
				human := newHumanATC()
				// Synth matches velocity filter (proto 101) so skip is what excludes it.
				synth := newSynth("SYN", false, protocol.NetworkRatingObserver, 101)
				for _, s := range []*session.Session{src, human, synth} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
					reg.UpdatePosition(s, pos, vr)
				}
				return reg, src, human, synth
			},
			invoke: func(reg *postoffice.PostOffice, src *session.Session) {
				broadcastRangedVelocity(reg, src, []byte(marker+"-vel\r\n"))
			},
		},
		{
			name: "broadcastRangedAtcOnly",
			setup: func(t *testing.T) (*postoffice.PostOffice, *session.Session, *session.Session, *session.Session) {
				reg := postoffice.New()
				src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC", ProtoRevision: 101})
				human := newHumanATC()
				// Synthetic ATC would pass IsAtc; Synthetic flag must still skip.
				synth := newSynth("SYN", true, protocol.NetworkRatingController1, 101)
				for _, s := range []*session.Session{src, human, synth} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
					reg.UpdatePosition(s, pos, vr)
				}
				return reg, src, human, synth
			},
			invoke: func(reg *postoffice.PostOffice, src *session.Session) {
				broadcastRangedAtcOnly(reg, src, []byte(marker+"-atc\r\n"))
			},
		},
		{
			name: "broadcastAll",
			setup: func(t *testing.T) (*postoffice.PostOffice, *session.Session, *session.Session, *session.Session) {
				reg := postoffice.New()
				src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC"})
				human := newHumanATC()
				synth := newSynth("SYN", false, protocol.NetworkRatingObserver, 100)
				for _, s := range []*session.Session{src, human, synth} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				return reg, src, human, synth
			},
			invoke: func(reg *postoffice.PostOffice, src *session.Session) {
				broadcastAll(reg, src, []byte(marker+"-all\r\n"))
			},
		},
		{
			name: "broadcastAllATC",
			setup: func(t *testing.T) (*postoffice.PostOffice, *session.Session, *session.Session, *session.Session) {
				reg := postoffice.New()
				src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC"})
				human := newHumanATC()
				synth := newSynth("SYN", true, protocol.NetworkRatingController1, 100)
				for _, s := range []*session.Session{src, human, synth} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				return reg, src, human, synth
			},
			invoke: func(reg *postoffice.PostOffice, src *session.Session) {
				broadcastAllATC(reg, src, []byte(marker+"-allatc\r\n"))
			},
		},
		{
			name: "broadcastAllSupervisors",
			setup: func(t *testing.T) (*postoffice.PostOffice, *session.Session, *session.Session, *session.Session) {
				reg := postoffice.New()
				src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC"})
				human := newHumanATC()
				// Synthetic supervisor-rated would pass rating filter without Synthetic skip.
				synth := newSynth("SYN", true, protocol.NetworkRatingSupervisor, 100)
				for _, s := range []*session.Session{src, human, synth} {
					if err := reg.Register(s); err != nil {
						t.Fatal(err)
					}
				}
				return reg, src, human, synth
			},
			invoke: func(reg *postoffice.PostOffice, src *session.Session) {
				broadcastAllSupervisors(reg, src, []byte(marker+"-sup\r\n"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, src, human, synth := tc.setup(t)
			tc.invoke(reg, src)

			humanOut := drain(human)
			if !hasOutboundContaining(humanOut, marker) {
				t.Fatalf("human recipient missed fan-out: %v", humanOut)
			}
			synthOut := drain(synth)
			if hasOutboundContaining(synthOut, marker) {
				t.Fatalf("synthetic recipient must be skipped: %v", synthOut)
			}
			// Source must not self-receive via registry Search/All.
			if hasOutboundContaining(drain(src), marker) {
				t.Fatalf("source should not receive its own fan-out: unexpected enqueue")
			}
		})
	}
}

// TestSendDirect_DoesNotSkipSynthetic documents that direct registry.Send
// still enqueues to Synthetic sessions (drain via SenderWorker in later PRs).
func TestSendDirect_DoesNotSkipSynthetic(t *testing.T) {
	reg := postoffice.New()
	src := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SRC"})
	synth := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "SYN"})
	synth.Synthetic = true
	for _, s := range []*session.Session{src, synth} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}
	sendDirectOrErr(reg, src, []byte("SYN"), []byte("direct-to-synth\r\n"))
	out := drain(synth)
	if !hasOutboundContaining(out, "direct-to-synth") {
		t.Fatalf("direct Send must still reach synthetic: %v", out)
	}
}

// TestBroadcastRangedVelocity_FiltersProto filters non-101 peers.
func TestBroadcastRangedVelocity_FiltersProto(t *testing.T) {
	reg := postoffice.New()
	src := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "SRC", ProtoRevision: 101,
	})
	v101 := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "V101", ProtoRevision: 101,
	})
	v100 := session.New(context.Background(), nil, nil, session.LoginData{
		Callsign: "V100", ProtoRevision: 100,
	})
	for _, s := range []*session.Session{src, v101, v100} {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
		reg.UpdatePosition(s, [2]float64{34, -118}, 100*1852)
	}

	broadcastRangedVelocity(reg, src, []byte("^SRC:34:-118:0:0:0:0:0:0:0:0:0:0\r\n"))
	if !hasOutboundContaining(drain(v101), "^SRC") {
		t.Fatal("proto-101 peer should receive velocity position")
	}
	if hasOutboundContaining(drain(v100), "^SRC") {
		t.Fatal("proto-100 peer must not receive velocity position")
	}
}

func TestSplitFieldsN(t *testing.T) {
	tests := []struct {
		name string
		pkt  string
		n    int
		ok   bool
		want []string
	}{
		{
			name: "full pilot position",
			pkt:  "@S:N100:1200:1:34.0:-118.0:5000:250:4261294148:0\r\n",
			n:    9,
			ok:   true,
			want: []string{"@S", "N100", "1200", "1", "34.0", "-118.0", "5000", "250", "4261294148"},
		},
		{
			name: "no trailing CRLF",
			pkt:  "a:b:c",
			n:    3,
			ok:   true,
			want: []string{"a", "b", "c"},
		},
		{
			name: "CR mid-packet ends body",
			pkt:  "a:b:c\r\nextra",
			n:    3,
			ok:   true,
			want: []string{"a", "b", "c"},
		},
		{
			name: "too few fields",
			pkt:  "a:b",
			n:    3,
			ok:   false,
		},
		{
			name: "empty fields preserved",
			pkt:  "a::c\r\n",
			n:    3,
			ok:   true,
			want: []string{"a", "", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([][]byte, tt.n)
			ok := splitFieldsN([]byte(tt.pkt), dst)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v", ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			for i := 0; i < tt.n; i++ {
				if string(dst[i]) != tt.want[i] {
					t.Fatalf("field[%d]=%q want %q (all=%q)", i, dst[i], tt.want[i], dst)
				}
			}
		})
	}
}

func TestFlightplanBuilders(t *testing.T) {
	fp := []byte("$FPN100:*A:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n")
	info := extractFlightplanInfoSection(fp)
	if !strings.HasPrefix(info, "I:B738") {
		t.Fatalf("fp info = %q", info)
	}
	am := []byte("$AMLAX_TWR:*A:N100:I:B738:430:KLAX:0000:0000:KSFO:0000:0000:0:0:0:0::/V/:\r\n")
	info2 := extractFlightplanInfoSection(am)
	if !strings.HasPrefix(info2, "I:B738") {
		t.Fatalf("am info = %q", info2)
	}

	p := buildFileFlightplanPacket("N100", "*A", info)
	if !strings.HasPrefix(p, "$FPN100:*A:") || !strings.HasSuffix(p, "\r\n") {
		t.Fatalf("file pkt = %q", p)
	}
	p2 := buildAmendFlightplanPacket("LAX_TWR", "*A", "N100", info)
	if !strings.HasPrefix(p2, "$AMLAX_TWR:*A:N100:") {
		t.Fatalf("amend pkt = %q", p2)
	}
	bc := buildBeaconCodePacket("server", "LAX_TWR", "N100", "1234")
	if !strings.Contains(bc, "BC:N100:1234") {
		t.Fatalf("bc = %q", bc)
	}

	s := session.New(context.Background(), nil, nil, session.LoginData{Callsign: "N100"})
	sendEnableSendFastPacket(s)
	sendDisableSendFastPacket(s)
	out := drain(s)
	if len(out) != 2 {
		t.Fatalf("sf packets = %v", out)
	}
	if !strings.Contains(out[0], ":1\r\n") || !strings.Contains(out[1], ":0\r\n") {
		t.Fatalf("sf content = %v", out)
	}

	if strPtr("x") == nil || *strPtr("x") != "x" {
		t.Fatal("strPtr")
	}
}
