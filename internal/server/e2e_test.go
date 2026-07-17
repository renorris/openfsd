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
	"sync"
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

func serviceClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}

// waitOnlinePilot polls GET /online_users until callsign appears (predicate, no bare sleep-assert).
func waitOnlinePilot(t *testing.T, ts *server.TestServer, callsign string, wantCID int) server.OnlineUserPilot {
	t.Helper()
	tok, err := ts.MakeServiceJWT()
	if err != nil {
		t.Fatal(err)
	}
	client := serviceClient()
	deadline := time.Now().Add(5 * time.Second)
	url := ts.HTTPBaseURL() + "/online_users"
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var data server.OnlineUsersResponseData
		err = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		for _, p := range data.Pilots {
			if p.Callsign == callsign && p.CID == wantCID {
				return p
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pilot %s (cid=%d) not in online_users within timeout", callsign, wantCID)
	return server.OnlineUserPilot{}
}

// nearKJFK returns a pilot position near KJFK.
func nearKJFK(cs string) protocol.PilotPosition {
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

// exchangeInRangePositions pumps @ positions until each peer sees the other.
// Joins both WaitFor goroutines before returning (including on failure).
func exchangeInRangePositions(t *testing.T, a, b *fsdclient.Client, callA, callB string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	seenB := make(chan error, 1)
	seenA := make(chan error, 1)
	go func() {
		defer wg.Done()
		_, err := a.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte(callB))
		})
		seenB <- err
	}()
	go func() {
		defer wg.Done()
		_, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte(callA))
		})
		seenA <- err
	}()

	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	gotA, gotB := false, false
	var errA, errB error
	for !gotA || !gotB {
		if err := a.SendPilotPosition(nearKJFK(callA)); err != nil {
			cancel()
			wg.Wait()
			t.Fatal(err)
		}
		if err := b.SendPilotPosition(nearKJFK(callB)); err != nil {
			cancel()
			wg.Wait()
			t.Fatal(err)
		}
		if !gotA {
			select {
			case errA = <-seenA:
				gotA = true
			default:
			}
		}
		if !gotB {
			select {
			case errB = <-seenB:
				gotB = true
			default:
			}
		}
		if gotA && gotB {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			cancel()
			wg.Wait()
			t.Fatalf("in-range exchange timeout gotA=%v gotB=%v (A=%v B=%v)",
				gotA, gotB, a.Recorder().Received(), b.Recorder().Received())
		}
	}
	cancel()
	wg.Wait()
	if errA != nil {
		t.Fatalf("B did not see A: %v (B recv=%v)", errA, b.Recorder().Received())
	}
	if errB != nil {
		t.Fatalf("A did not see B: %v (A recv=%v)", errB, a.Recorder().Received())
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

// TestE2E_ServerCAPS mirrors vatSys post-login $CQ {cs}:SERVER:CAPS and expects
// $CRSERVER:{cs}:CAPS:… with the openfsd capability set (no SECPOS).
func TestE2E_ServerCAPS(t *testing.T) {
	ts := server.StartTestServer(t)
	c := dial(t, ts)

	const cs = "CAPS_TWR"
	loginATC(t, c, cs, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, c, cs)

	if err := c.Send([]byte("$CQ" + cs + ":SERVER:CAPS\r\n")); err != nil {
		t.Fatalf("send CAPS query: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wantPrefix := []byte("$CRSERVER:" + cs + ":CAPS:")
	r, err := c.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeClientQueryResponse &&
			bytes.HasPrefix(r.Raw, wantPrefix)
	})
	if err != nil {
		t.Fatalf("wait CAPS response: %v (recorder=%v)", err, c.Recorder().All())
	}
	raw := string(r.Raw)
	if !strings.Contains(raw, server.ServerCapabilitiesPayload()) {
		t.Fatalf("CAPS payload mismatch: got %q want contains %q", raw, server.ServerCapabilitiesPayload())
	}
	if strings.Contains(raw, "SECPOS=") {
		t.Fatalf("server CAPS must not advertise SECPOS: %q", raw)
	}
	// Pilot path also gets CAPS (vatSys-compatible for any client type)
	c2 := dial(t, ts)
	loginPilot(t, c2, "CAPS_PLT", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c2, "CAPS_PLT")
	if err := c2.Send([]byte("$CQCAPS_PLT:SERVER:CAPS\r\n")); err != nil {
		t.Fatalf("pilot CAPS query: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, err = c2.WaitFor(ctx2, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeClientQueryResponse &&
			bytes.HasPrefix(r.Raw, []byte("$CRSERVER:CAPS_PLT:CAPS:"))
	})
	if err != nil {
		t.Fatalf("wait pilot CAPS: %v (recorder=%v)", err, c2.Recorder().All())
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

	exchangeInRangePositions(t, a, b, "NEAR_A", "NEAR_B")

	// Far client should not receive near traffic.
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
	// Predicate-poll until snapshot shows FAR_C geo in southern hemisphere (indexed).
	tok, err := ts.MakeServiceJWT()
	if err != nil {
		t.Fatal(err)
	}
	httpCl := serviceClient()
	indexDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(indexDeadline) {
		if err := c.SendPilotPosition(farPos); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodGet, ts.HTTPBaseURL()+"/online_users", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := httpCl.Do(req)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var data server.OnlineUsersResponseData
		_ = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		for _, pl := range data.Pilots {
			if pl.Callsign == "FAR_C" && pl.Latitude < -30 {
				goto farIndexed
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("FAR_C geo not indexed in online_users")
farIndexed:

	// Multi-send absence window: repeatedly send NEAR_A while asserting FAR_C never gets it.
	absCtx, absCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer absCancel()
	hit := make(chan struct{}, 1)
	go func() {
		_, err := c.WaitFor(absCtx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte("NEAR_A"))
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
			t.Fatal("FAR_C unexpectedly received NEAR_A position")
		case <-absCtx.Done():
			return // timeout with no hit = pass
		case <-ticker.C:
			if err := a.SendPilotPosition(nearKJFK("NEAR_A")); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestE2E_FrequencyBroadcast(t *testing.T) {
	ts := server.StartTestServer(t)

	a := dial(t, ts)
	loginPilot(t, a, "FREQ_A", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, a, "FREQ_A")

	b := dial(t, ts)
	loginPilot(t, b, "FREQ_B", ts.Pilot2CID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, b, "FREQ_B")

	// Place both in range so @-recipient text uses broadcastRanged.
	exchangeInRangePositions(t, a, b, "FREQ_A", "FREQ_B")

	// Far pilot should not receive frequency traffic.
	far := dial(t, ts)
	loginPilot(t, far, "FREQ_F", ts.SupCID, ts.SupPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, far, "FREQ_F")
	if err := far.SendPilotPosition(protocol.PilotPosition{
		TransponderMode: "S",
		Callsign:        "FREQ_F",
		TransponderCode: "1200",
		NetworkRating:   protocol.NetworkRatingObserver,
		Latitude:        -33.95,
		Longitude:       151.18,
		TrueAltitude:    3000,
		Groundspeed:     100,
	}); err != nil {
		t.Fatal(err)
	}
	// Wait until far is online (geo presence) without bare sleep-assert.
	_ = waitOnlinePilot(t, ts, "FREQ_F", ts.SupCID)

	const freqMsg = "freq traffic e2e"
	// Frequency-addressed text: recipient starts with @
	if err := a.Send([]byte("#TMFREQ_A:@22800:" + freqMsg)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := b.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeTextMessage && bytes.Contains(r.Raw, []byte(freqMsg))
	})
	if err != nil {
		t.Fatalf("in-range peer did not receive frequency broadcast: %v (recv=%v)", err, b.Recorder().Received())
	}
	if !bytes.Contains(got.Raw, []byte("@22800")) {
		t.Fatalf("unexpected frequency wire: %q", got.Raw)
	}

	// Absence window for far client.
	absCtx, absCancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer absCancel()
	if _, err := far.WaitFor(absCtx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeTextMessage && bytes.Contains(r.Raw, []byte(freqMsg))
	}); err == nil {
		t.Fatal("far client unexpectedly received frequency broadcast")
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
	// Note: handleDelete broadcasts then Cancel; defer broadcastDisconnectPacket may
	// emit a second #DP — peer WaitFor accepts the first matching delete.
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
		// $AR is not in protocol.TypeOf; match wire prefix + station.
		return bytes.HasPrefix(bytes.TrimRight(r.Raw, "\r\n"), []byte("$AR")) &&
			bytes.Contains(r.Raw, []byte("KJFK"))
	})
	if err != nil {
		t.Fatalf("METAR response: %v (recorder=%v)", err, c.Recorder().All())
	}
	if !bytes.Contains(got.Raw, []byte("18010KT")) {
		t.Fatalf("unexpected METAR body: %q", got.Raw)
	}
	// Mock is path-keyed; wrong station would 404 and yield $ER — success implies /KJFK.TXT path.
	if !bytes.Contains(got.Raw, []byte("METAR:")) {
		t.Fatalf("missing METAR field: %q", got.Raw)
	}
}

func TestE2E_ServiceHTTPOnlineUsersAndKick(t *testing.T) {
	ts := server.StartTestServer(t)

	c := dial(t, ts)
	loginPilot(t, c, "HTTP1", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, c, "HTTP1")

	const wantLat, wantLon = 40.0, -74.0
	const wantAlt = 5000
	if err := c.SendPilotPosition(protocol.PilotPosition{
		TransponderMode: "S",
		Callsign:        "HTTP1",
		TransponderCode: "2000",
		NetworkRating:   protocol.NetworkRatingObserver,
		Latitude:        wantLat,
		Longitude:       wantLon,
		TrueAltitude:    wantAlt,
		Groundspeed:     200,
	}); err != nil {
		t.Fatal(err)
	}

	// Poll until snapshot shows callsign with updated coordinates (no bare sleep).
	tok, err := ts.MakeServiceJWT()
	if err != nil {
		t.Fatal(err)
	}
	client := serviceClient()
	deadline := time.Now().Add(5 * time.Second)
	var pilot server.OnlineUserPilot
	found := false
	for time.Now().Before(deadline) {
		_ = c.SendPilotPosition(protocol.PilotPosition{
			TransponderMode: "S",
			Callsign:        "HTTP1",
			TransponderCode: "2000",
			NetworkRating:   protocol.NetworkRatingObserver,
			Latitude:        wantLat,
			Longitude:       wantLon,
			TrueAltitude:    wantAlt,
			Groundspeed:     200,
		})
		req, err := http.NewRequest(http.MethodGet, ts.HTTPBaseURL()+"/online_users", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var data server.OnlineUsersResponseData
		err = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		if err != nil {
			continue
		}
		for _, p := range data.Pilots {
			if p.Callsign == "HTTP1" && p.CID == ts.PilotCID &&
				p.Latitude == wantLat && p.Longitude == wantLon {
				pilot = p
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatalf("HTTP1 with lat/lon not in online_users")
	}
	if pilot.Altitude != wantAlt {
		t.Fatalf("altitude = %d, want %d", pilot.Altitude, wantAlt)
	}

	// Kick via service HTTP
	kickBody := strings.NewReader(`{"callsign":"HTTP1"}`)
	kickReq, err := http.NewRequest(http.MethodPost, ts.HTTPBaseURL()+"/kick_user", kickBody)
	if err != nil {
		t.Fatal(err)
	}
	kickReq.Header.Set("Authorization", "Bearer "+tok)
	kickReq.Header.Set("Content-Type", "application/json")
	kickResp, err := client.Do(kickReq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(kickResp.Body)
	_ = kickResp.Body.Close()
	if kickResp.StatusCode != http.StatusNoContent {
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
