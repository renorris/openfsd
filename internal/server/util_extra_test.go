package server

import (
	"context"
	"strings"
	"testing"

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
	vr, ok := parseVisRange(pkt, 3)
	if !ok || vr != 40*1852 {
		t.Fatalf("visRange = %v ok=%v", vr, ok)
	}
	if _, ok := parseVisRange([]byte("a:b:c:xx"), 3); ok {
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
