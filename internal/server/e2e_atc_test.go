package server_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/pkg/fsdclient"
	"github.com/renorris/openfsd/pkg/protocol"
)

// TestE2E_ATCSelfQueryBeforePosition covers ATC-RACE: vatSys sends
// $CQ SERVER:ATC (self) before the first % position. Must answer Y for
// controller-rated ATC so ValidATC is not stuck false.
func TestE2E_ATCSelfQueryBeforePosition(t *testing.T) {
	ts := server.StartTestServer(t)

	const cs = "RACE_TWR"
	c := dial(t, ts)
	loginATC(t, c, cs, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, c, cs)

	// Immediately self-query — no % yet (FacilityType still 0).
	if err := c.Send([]byte("$CQ" + cs + ":SERVER:ATC:" + cs + "\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeClientQueryResponse &&
			bytes.Contains(r.Raw, []byte("ATC:Y:")) &&
			bytes.Contains(r.Raw, []byte(cs))
	})
	if err != nil {
		t.Fatalf("self ATC query before position: %v (recorder=%v)", err, c.Recorder().All())
	}
	_ = r

	// First position still works afterward.
	pos := protocol.ATCPosition{
		Callsign:        cs,
		Frequencies:     "28550",
		FacilityType:    4,
		VisibilityRange: 40,
		NetworkRating:   protocol.NetworkRatingController1,
		Latitude:        34.05,
		Longitude:       -118.25,
	}
	if err := c.SendATCPosition(pos); err != nil {
		t.Fatal(err)
	}
}

// TestE2E_ATCInRangeVisibility: ATC % in range is visible to pilot and peer ATC.
func TestE2E_ATCInRangeVisibility(t *testing.T) {
	ts := server.StartTestServer(t)

	const atcCS = "VIS_TWR"
	const peerCS = "VIS_GND"
	const pilotCS = "VIS_PLT"

	atc := dial(t, ts)
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)

	peer := dial(t, ts)
	// Second ATC must use a controller-capable account (Sup seed).
	loginATC(t, peer, peerCS, ts.SupCID, ts.SupPassword, protocol.NetworkRatingSupervisor)
	waitMOTD(t, peer, peerCS)

	pilot := dial(t, ts)
	loginPilot(t, pilot, pilotCS, ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, pilot, pilotCS)

	// Seed pilot and peer geo near KJFK.
	pilotPos := nearKJFK(pilotCS)
	if err := pilot.SendPilotPosition(pilotPos); err != nil {
		t.Fatal(err)
	}
	peerPos := protocol.ATCPosition{
		Callsign:        peerCS,
		Frequencies:     "21770",
		FacilityType:    3,
		VisibilityRange: 40,
		NetworkRating:   protocol.NetworkRatingSupervisor,
		Latitude:        pilotPos.Latitude,
		Longitude:       pilotPos.Longitude,
	}
	if err := peer.SendATCPosition(peerPos); err != nil {
		t.Fatal(err)
	}

	// ATC position at same geo. Re-send until both pilot and peer observe it
	// (gnet can process peers' geo after a single ATC % has already fanned out).
	atcPos := protocol.ATCPosition{
		Callsign:        atcCS,
		Frequencies:     "28550",
		FacilityType:    4,
		VisibilityRange: 40,
		NetworkRating:   protocol.NetworkRatingController1,
		Latitude:        pilotPos.Latitude,
		Longitude:       pilotPos.Longitude,
	}

	waitATCPos := func(t *testing.T, c *fsdclient.Client, who string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hit := make(chan struct{}, 1)
		go func() {
			_, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
				return r.Type == protocol.PacketTypeATCPosition && bytes.Contains(r.Raw, []byte(atcCS))
			})
			if err == nil {
				select {
				case hit <- struct{}{}:
				default:
				}
			}
		}()
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-hit:
				return
			case <-ctx.Done():
				t.Fatalf("%s wait ATC position: %v (recorder=%v)", who, ctx.Err(), c.Recorder().All())
			case <-ticker.C:
				if err := atc.SendATCPosition(atcPos); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	waitATCPos(t, pilot, "pilot")
	waitATCPos(t, peer, "peer ATC")
}

// TestE2E_HandoffForward: $HO then $HA direct-forward between two ATCs.
func TestE2E_HandoffForward(t *testing.T) {
	ts := server.StartTestServer(t)

	const app = "HO_APP"
	const ctr = "HO_CTR"
	const ac = "HO_PLT"

	a := dial(t, ts)
	loginATC(t, a, app, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, a, app)

	b := dial(t, ts)
	loginATC(t, b, ctr, ts.SupCID, ts.SupPassword, protocol.NetworkRatingSupervisor)
	waitMOTD(t, b, ctr)

	// Facility > OBS required for handoff.
	posA := protocol.ATCPosition{
		Callsign: app, Frequencies: "28550", FacilityType: 5, VisibilityRange: 50,
		NetworkRating: protocol.NetworkRatingController1, Latitude: 34.0, Longitude: -118.0,
	}
	posB := protocol.ATCPosition{
		Callsign: ctr, Frequencies: "28550", FacilityType: 6, VisibilityRange: 150,
		NetworkRating: protocol.NetworkRatingSupervisor, Latitude: 34.0, Longitude: -118.0,
	}
	if err := a.SendATCPosition(posA); err != nil {
		t.Fatal(err)
	}
	if err := b.SendATCPosition(posB); err != nil {
		t.Fatal(err)
	}

	if err := a.Send([]byte("$HO" + app + ":" + ctr + ":" + ac + "\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeHandoffRequest &&
			bytes.Contains(r.Raw, []byte(app)) &&
			bytes.Contains(r.Raw, []byte(ac))
	}); err != nil {
		t.Fatalf("CTR wait $HO: %v (recorder=%v)", err, b.Recorder().All())
	}

	if err := b.Send([]byte("$HA" + ctr + ":" + app + ":" + ac + "\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := a.WaitFor(ctx2, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeHandoffAccept &&
			bytes.Contains(r.Raw, []byte(ctr)) &&
			bytes.Contains(r.Raw, []byte(ac))
	}); err != nil {
		t.Fatalf("APP wait $HA: %v (recorder=%v)", err, a.Recorder().All())
	}

	// $HC cancel also forwards.
	if err := a.Send([]byte("$HC" + app + ":" + ctr + ":" + ac + "\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel3()
	if _, err := b.WaitFor(ctx3, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeHandoffCancel &&
			bytes.Contains(r.Raw, []byte(app)) &&
			bytes.Contains(r.Raw, []byte(ac))
	}); err != nil {
		t.Fatalf("CTR wait $HC: %v (recorder=%v)", err, b.Recorder().All())
	}
}

// TestE2E_ATCChatAndITRange: ATC chat @49999 and $CQ IT to @94835 ATC-only range.
func TestE2E_ATCChatAndITRange(t *testing.T) {
	ts := server.StartTestServer(t)

	const aCS = "CHAT_TWR"
	const bCS = "CHAT_GND"
	const pilotCS = "CHAT_PLT"

	a := dial(t, ts)
	loginATC(t, a, aCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, a, aCS)

	b := dial(t, ts)
	loginATC(t, b, bCS, ts.SupCID, ts.SupPassword, protocol.NetworkRatingSupervisor)
	waitMOTD(t, b, bCS)

	pilot := dial(t, ts)
	loginPilot(t, pilot, pilotCS, ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, pilot, pilotCS)

	lat, lon := 40.64, -73.78
	for _, send := range []func() error{
		func() error {
			return a.SendATCPosition(protocol.ATCPosition{
				Callsign: aCS, Frequencies: "28550", FacilityType: 4, VisibilityRange: 40,
				NetworkRating: protocol.NetworkRatingController1, Latitude: lat, Longitude: lon,
			})
		},
		func() error {
			return b.SendATCPosition(protocol.ATCPosition{
				Callsign: bCS, Frequencies: "21770", FacilityType: 3, VisibilityRange: 40,
				NetworkRating: protocol.NetworkRatingSupervisor, Latitude: lat, Longitude: lon,
			})
		},
		func() error {
			return pilot.SendPilotPosition(protocol.PilotPosition{
				TransponderMode: "S", Callsign: pilotCS, TransponderCode: "1200",
				NetworkRating: protocol.NetworkRatingObserver,
				Latitude:      lat, Longitude: lon, TrueAltitude: 1000, Groundspeed: 0,
			})
		},
	} {
		if err := send(); err != nil {
			t.Fatal(err)
		}
	}

	// ATC-only chat.
	if err := a.Send([]byte("#TM" + aCS + ":@49999:hello controllers\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeTextMessage &&
			bytes.Contains(r.Raw, []byte("@49999")) &&
			bytes.Contains(r.Raw, []byte("hello controllers"))
	}); err != nil {
		t.Fatalf("peer ATC chat: %v (recorder=%v)", err, b.Recorder().All())
	}
	// Pilot must not receive ATC chat.
	time.Sleep(100 * time.Millisecond)
	for _, r := range pilot.Recorder().All() {
		if r.Type == protocol.PacketTypeTextMessage && bytes.Contains(r.Raw, []byte("hello controllers")) {
			t.Fatalf("pilot must not receive @49999 ATC chat: %q", r.Raw)
		}
	}

	// $CQ IT to @94835 — ATC-only range broadcast.
	if err := a.Send([]byte("$CQ" + aCS + ":@94835:IT:" + pilotCS + "\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := b.WaitFor(ctx2, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeClientQuery &&
			bytes.Contains(r.Raw, []byte(":IT:")) &&
			bytes.Contains(r.Raw, []byte(pilotCS))
	}); err != nil {
		t.Fatalf("peer ATC IT: %v (recorder=%v)", err, b.Recorder().All())
	}
	time.Sleep(100 * time.Millisecond)
	for _, r := range pilot.Recorder().All() {
		if r.Type == protocol.PacketTypeClientQuery && bytes.Contains(r.Raw, []byte(":IT:")) {
			t.Fatalf("pilot must not receive @94835 IT: %q", r.Raw)
		}
	}
}

// TestE2E_BeaconAssignAndFPRequest: assign BC then $CQ SERVER:FP returns non-zero code.
func TestE2E_BeaconAssignAndFPRequest(t *testing.T) {
	ts := server.StartTestServer(t)

	const atcCS = "BC_TWR"
	const pilotCS = "BC_PLT"

	atc := dial(t, ts)
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)

	pilot := dial(t, ts)
	loginPilot(t, pilot, pilotCS, ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, pilot, pilotCS)

	// File a flight plan so SERVER:FP has something to return.
	fpl := "$FP" + pilotCS + ":*A:I:B738:430:KJFK:1200:0000:350:KLAX:0500:0000:0:0:0:0::/V/:\r\n"
	if err := pilot.Send([]byte(fpl)); err != nil {
		t.Fatal(err)
	}

	// ATC needs facility > OBS for privileged BC.
	if err := atc.SendATCPosition(protocol.ATCPosition{
		Callsign: atcCS, Frequencies: "28550", FacilityType: 4, VisibilityRange: 50,
		NetworkRating: protocol.NetworkRatingController1, Latitude: 40.64, Longitude: -73.78,
	}); err != nil {
		t.Fatal(err)
	}

	// Assign beacon via $CQ range form.
	if err := atc.Send([]byte("$CQ" + atcCS + ":@94835:BC:" + pilotCS + ":7032\r\n")); err != nil {
		t.Fatal(err)
	}

	// Re-request flight plan + beacon from SERVER.
	if err := atc.Send([]byte("$CQ" + atcCS + ":SERVER:FP:" + pilotCS + "\r\n")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := atc.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeFlightPlan && bytes.Contains(r.Raw, []byte(pilotCS))
	}); err != nil {
		t.Fatalf("wait $FP: %v (recorder=%v)", err, atc.Recorder().All())
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _, err := atc.WaitFor(ctx2, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeProController &&
			bytes.Contains(r.Raw, []byte("BC:"+pilotCS+":7032"))
	}); err != nil {
		t.Fatalf("wait beacon on FP re-request: %v (recorder=%v)", err, atc.Recorder().All())
	}
}

// TestE2E_UnknownPacketNoER: unknown prefix is soft-dropped (no $ER spam).
func TestE2E_UnknownPacketNoER(t *testing.T) {
	ts := server.StartTestServer(t)

	c := dial(t, ts)
	loginPilot(t, c, "UNK_PLT", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "UNK_PLT")

	if err := c.Send([]byte("#WXUNK_PLT:SERVER:KJFK\r\n")); err != nil {
		t.Fatal(err)
	}
	// Brief window: must not receive $ER for unknown type.
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		rctx, rcancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		r, err := c.Next(rctx)
		rcancel()
		if err != nil {
			continue
		}
		if r.Type == protocol.PacketTypeError {
			t.Fatalf("unexpected $ER for unknown packet: %q", r.Raw)
		}
	}
}

// TestE2E_TextToFPAndServerSilent: #TM to FP/SERVER is dropped (no $ER, no fan-out).
func TestE2E_TextToFPAndServerSilent(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginATC(t, a, "TM_TWR", ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, a, "TM_TWR")

	b := dial(t, ts)
	loginPilot(t, b, "TM_PLT", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "TM_PLT")

	if err := a.Send([]byte("#TMTM_TWR:FP:TM_PLT release\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := a.Send([]byte("#TMTM_TWR:SERVER:hello\r\n")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)
	for _, r := range b.Recorder().All() {
		if bytes.Contains(r.Raw, []byte("release")) || bytes.Contains(r.Raw, []byte("hello")) {
			t.Fatalf("peer must not receive FP/SERVER text: %q", r.Raw)
		}
	}
	for _, r := range a.Recorder().All() {
		if r.Type == protocol.PacketTypeError {
			t.Fatalf("FP/SERVER text must not $ER: %q", r.Raw)
		}
	}
}
