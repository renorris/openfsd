package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/pkg/fsdclient"
	"github.com/renorris/openfsd/pkg/protocol"
)

// defaultClientIdent builds a minimal $ID payload (required by openfsd login).
func defaultClientIdent() *protocol.ClientIdent {
	return &protocol.ClientIdent{
		SoftwareID:   "88e4",
		SoftwareName: "e2e",
		VersionMajor: 1,
		VersionMinor: 0,
		SystemUID:    1,
	}
}

func dial(t *testing.T, ts *server.TestServer) *fsdclient.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := fsdclient.Dial(ctx, fsdclient.Config{
		Addr:        ts.FSDAddr,
		DialTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func waitMOTD(t *testing.T, c *fsdclient.Client, callsign string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeTextMessage &&
			bytes.Contains(r.Raw, []byte(callsign)) &&
			bytes.Contains(r.Raw, []byte("Connected to openfsd"))
	})
	if err != nil {
		t.Fatalf("wait MOTD for %s: %v (last recorder=%v)", callsign, err, c.Recorder().All())
	}
	_ = r
}

func waitError(t *testing.T, c *fsdclient.Client, code protocol.ErrorCode) fsdclient.Received {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	want := fmt.Sprintf(":%d:", int(code))
	r, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeError && bytes.Contains(r.Raw, []byte(want))
	})
	if err != nil {
		t.Fatalf("wait error %d: %v (recorder=%v)", code, err, c.Recorder().All())
	}
	return r
}

func loginPilot(t *testing.T, c *fsdclient.Client, callsign string, cid int, token string, rating protocol.NetworkRating) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.LoginPilot(ctx, fsdclient.PilotLogin{
		Callsign:      callsign,
		CID:           strconv.Itoa(cid),
		Token:         token,
		NetworkRating: rating,
		ProtoRevision: 100,
		SimulatorType: 1,
		RealName:      "E2E Pilot",
		ClientIdent:   defaultClientIdent(),
	})
	if err != nil {
		t.Fatalf("LoginPilot %s: %v", callsign, err)
	}
}

func loginATC(t *testing.T, c *fsdclient.Client, callsign string, cid int, token string, rating protocol.NetworkRating) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.LoginATC(ctx, fsdclient.ATCLogin{
		Callsign:      callsign,
		CID:           strconv.Itoa(cid),
		Token:         token,
		NetworkRating: rating,
		ProtoRevision: 100,
		RealName:      "E2E ATC",
		ClientIdent:   defaultClientIdent(),
	})
	if err != nil {
		t.Fatalf("LoginATC %s: %v", callsign, err)
	}
}

func TestE2E_PilotLoginPassword(t *testing.T) {
	ts := server.StartTestServer(t)
	c := dial(t, ts)

	loginPilot(t, c, "N100E2E", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "N100E2E")

	if c.Callsign() != "N100E2E" {
		t.Fatalf("callsign = %q", c.Callsign())
	}
	if c.IsATC() {
		t.Fatal("expected pilot, got ATC")
	}
	ident := c.ServerIdent()
	if ident.Version != "openfsd" {
		t.Fatalf("server version = %q", ident.Version)
	}
}

func TestE2E_ATCLogin(t *testing.T) {
	ts := server.StartTestServer(t)
	c := dial(t, ts)

	loginATC(t, c, "E2E_TWR", ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, c, "E2E_TWR")

	if !c.IsATC() {
		t.Fatal("expected ATC")
	}
}

func TestE2E_DuplicateCallsignRejected(t *testing.T) {
	ts := server.StartTestServer(t)

	c1 := dial(t, ts)
	loginPilot(t, c1, "DUPESIGN", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c1, "DUPESIGN")

	c2 := dial(t, ts)
	loginPilot(t, c2, "DUPESIGN", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitError(t, c2, protocol.CallsignInUseError)
}

func TestE2E_BadPasswordRejected(t *testing.T) {
	ts := server.StartTestServer(t)
	c := dial(t, ts)

	loginPilot(t, c, "BADPASS1", ts.PilotCID, "wrong-password", protocol.NetworkRatingObserver)
	waitError(t, c, protocol.InvalidLogonError)
}

func TestE2E_JWTLogin(t *testing.T) {
	ts := server.StartTestServer(t)
	jwt, err := ts.MakeFSDJWT(ts.PilotCID, protocol.NetworkRatingObserver)
	if err != nil {
		t.Fatal(err)
	}

	c := dial(t, ts)
	loginPilot(t, c, "JWTPILOT", ts.PilotCID, jwt, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "JWTPILOT")
}

func TestE2E_InRangeVisibility(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "NEAR_A", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "NEAR_A")

	b := dial(t, ts)
	loginPilot(t, b, "NEAR_B", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "NEAR_B")

	// Drain B's view of A's add packet if any, then exchange positions near KJFK.
	nearPos := func(cs string) protocol.PilotPosition {
		return protocol.PilotPosition{
			TransponderMode:    "S",
			Callsign:           cs,
			TransponderCode:    "1200",
			NetworkRating:      protocol.NetworkRatingObserver,
			Latitude:           40.64,
			Longitude:          -73.78,
			TrueAltitude:       1000,
			Groundspeed:        180,
			PitchBankHeading:   0,
			AltitudeCorrection: 0,
		}
	}

	// Seed both into the geo index near KJFK, then re-broadcast until each
	// sees the other (Send returns before the server finishes handling).
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	seenB := make(chan error, 1)
	go func() {
		_, err := a.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte("NEAR_B"))
		})
		seenB <- err
	}()
	seenA := make(chan error, 1)
	go func() {
		_, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte("NEAR_A"))
		})
		seenA <- err
	}()

	// Pump positions until both receivers succeed or ctx expires.
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	var errA, errB error
	gotA, gotB := false, false
	for !gotA || !gotB {
		if err := a.SendPilotPosition(nearPos("NEAR_A")); err != nil {
			t.Fatal(err)
		}
		if err := b.SendPilotPosition(nearPos("NEAR_B")); err != nil {
			t.Fatal(err)
		}
		select {
		case errA = <-seenA:
			gotA = true
			if errA != nil {
				t.Fatalf("B did not see A: %v (B recv=%v)", errA, b.Recorder().Received())
			}
		case errB = <-seenB:
			gotB = true
			if errB != nil {
				t.Fatalf("A did not see B: %v (A recv=%v)", errB, a.Recorder().Received())
			}
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("in-range exchange timeout gotA=%v gotB=%v (A=%v B=%v)",
				gotA, gotB, a.Recorder().Received(), b.Recorder().Received())
		}
	}

	// Far client should not receive near traffic.
	// Reuse supervisor CID as a third pilot account isn't available; use SUP with observer rating
	// — rating may be up to max, so login as observer with sup credentials is fine if rating <= max.
	c := dial(t, ts)
	loginPilot(t, c, "FAR_C", ts.SupCID, ts.SupPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "FAR_C")

	farPos := protocol.PilotPosition{
		TransponderMode:  "S",
		Callsign:         "FAR_C",
		TransponderCode:  "1200",
		NetworkRating:    protocol.NetworkRatingObserver,
		Latitude:         -33.95,
		Longitude:        151.18,
		TrueAltitude:     5000,
		Groundspeed:      250,
		PitchBankHeading: 0,
	}
	if err := c.SendPilotPosition(farPos); err != nil {
		t.Fatal(err)
	}
	// Give server time to index FAR_C position.
	time.Sleep(50 * time.Millisecond)

	if err := a.SendPilotPosition(nearPos("NEAR_A")); err != nil {
		t.Fatal(err)
	}

	short, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if _, err := c.WaitFor(short, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte("NEAR_A"))
	}); err == nil {
		t.Fatal("FAR_C unexpectedly received NEAR_A position")
	}
}

func TestE2E_TextDM(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "DMA", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "DMA")

	b := dial(t, ts)
	loginPilot(t, b, "DMB", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "DMB")

	msg := "#TMDMA:DMB:hello from A"
	if err := a.Send([]byte(msg)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeTextMessage && bytes.Contains(r.Raw, []byte("hello from A"))
	})
	if err != nil {
		t.Fatalf("DM not received: %v", err)
	}
	if !bytes.Contains(got.Raw, []byte("#TMDMA:DMB:")) {
		t.Fatalf("unexpected DM wire: %q", got.Raw)
	}
}

func TestE2E_FlightPlan(t *testing.T) {
	ts := server.StartTestServer(t)

	pilot := dial(t, ts)
	loginPilot(t, pilot, "FPPILOT", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, pilot, "FPPILOT")

	atc := dial(t, ts)
	loginATC(t, atc, "FP_TWR", ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, "FP_TWR")

	// 17 fields minimum for $FP
	fp := "$FPFPPILOT:SERVER:I:B738/L:420:KJFK:1200:1205:35000:KLAX:5:30:6:0:KPHX:RMK/E2E:DCT"
	if err := pilot.Send([]byte(fp)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := atc.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeFlightPlan &&
			bytes.Contains(r.Raw, []byte("FPPILOT")) &&
			bytes.Contains(r.Raw, []byte("KJFK"))
	})
	if err != nil {
		t.Fatalf("ATC did not receive flight plan: %v (recorder=%v)", err, atc.Recorder().All())
	}
	if !bytes.Contains(got.Raw, []byte("*A")) {
		t.Fatalf("expected *A recipient in broadcast FP, got %q", got.Raw)
	}
}

func TestE2E_Disconnect(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "DISCA", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "DISCA")

	b := dial(t, ts)
	loginPilot(t, b, "DISCB", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "DISCB")

	// openfsd verifyPacket requires ≥3 fields; wire #DP with SERVER + CID.
	// Closing also triggers broadcastDisconnectPacket via handleConn defer.
	if err := a.Send([]byte("#DPDISCA:SERVER:" + strconv.Itoa(ts.PilotCID))); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeDeletePilot && bytes.Contains(r.Raw, []byte("DISCA"))
	})
	if err != nil {
		t.Fatalf("B did not see disconnect: %v (recorder=%v)", err, b.Recorder().All())
	}
}

func TestE2E_KillSupervisor(t *testing.T) {
	ts := server.StartTestServer(t)

	victim := dial(t, ts)
	loginPilot(t, victim, "VICTIM1", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, victim, "VICTIM1")

	sup := dial(t, ts)
	loginATC(t, sup, "E2E_SUP", ts.SupCID, ts.SupPassword, protocol.NetworkRatingSupervisor)
	waitMOTD(t, sup, "E2E_SUP")

	if err := sup.Send([]byte("$!!E2E_SUP:VICTIM1:e2e kill")); err != nil {
		t.Fatal(err)
	}

	// Victim connection should close (context cancel on server side).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := victim.Next(ctx)
	if err == nil {
		// Drain until error/close
		for {
			_, err = victim.Next(ctx)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		t.Fatal("expected victim connection to end after kill")
	}
}

func TestE2E_METARMock(t *testing.T) {
	ts := server.StartTestServer(t)
	c := dial(t, ts)
	loginPilot(t, c, "METAR1", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "METAR1")

	if err := c.Send([]byte("$AXMETAR1:SERVER:METAR:KJFK")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
		// $AR not classified by TypeOf as a known type always — match raw.
		return bytes.HasPrefix(bytes.TrimRight(r.Raw, "\r\n"), []byte("$AR")) &&
			bytes.Contains(r.Raw, []byte("KJFK"))
	})
	if err != nil {
		t.Fatalf("METAR response: %v (recorder=%v)", err, c.Recorder().All())
	}
	if !bytes.Contains(got.Raw, []byte("18010KT")) {
		t.Fatalf("unexpected METAR body: %q", got.Raw)
	}
}

func TestE2E_ServiceHTTPOnlineUsersAndKick(t *testing.T) {
	ts := server.StartTestServer(t)

	c := dial(t, ts)
	loginPilot(t, c, "HTTP1", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "HTTP1")

	// Position so snapshot has lat/lon.
	if err := c.SendPilotPosition(protocol.PilotPosition{
		TransponderMode: "S",
		Callsign:        "HTTP1",
		TransponderCode: "2000",
		NetworkRating:   protocol.NetworkRatingObserver,
		Latitude:        40.0,
		Longitude:       -74.0,
		TrueAltitude:    5000,
		Groundspeed:     200,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	svcTok, err := ts.MakeServiceJWT()
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodGet, ts.HTTPBaseURL()+"/online_users", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+svcTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("online_users status %d body %s", resp.StatusCode, body)
	}

	var data server.OnlineUsersResponseData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range data.Pilots {
		if p.Callsign == "HTTP1" && p.CID == ts.PilotCID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("HTTP1 not in online_users: %+v", data)
	}

	// Kick via service HTTP
	kickBody := strings.NewReader(`{"callsign":"HTTP1"}`)
	kickReq, err := http.NewRequest(http.MethodPost, ts.HTTPBaseURL()+"/kick_user", kickBody)
	if err != nil {
		t.Fatal(err)
	}
	kickReq.Header.Set("Authorization", "Bearer "+svcTok)
	kickReq.Header.Set("Content-Type", "application/json")
	kickResp, err := http.DefaultClient.Do(kickReq)
	if err != nil {
		t.Fatal(err)
	}
	defer kickResp.Body.Close()
	if kickResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(kickResp.Body)
		t.Fatalf("kick status %d body %s", kickResp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.Next(ctx)
	if err == nil {
		for {
			_, err = c.Next(ctx)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		t.Fatal("expected client disconnect after HTTP kick")
	}
}
