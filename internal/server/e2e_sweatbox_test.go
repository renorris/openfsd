package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/server"
	"github.com/renorris/openfsd/internal/serviceapi"
	"github.com/renorris/openfsd/pkg/fsdclient"
	"github.com/renorris/openfsd/pkg/protocol"
)

// KBTV field center (approx) for ATC/pilot geo placement.
const (
	kbtvLat = 44.47
	kbtvLon = -73.15
)

// sweatboxDefaultCID is the host default (SWEATBOX_CID / 900001).
const sweatboxDefaultCID = 900001

// ---------------------------------------------------------------------------
// Sweatbox service-HTTP helpers (e2e, real StartTestServer)
// ---------------------------------------------------------------------------

func readSweatboxFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "sweatbox", "testdata", name)
	b, err := os.ReadFile(path)
	if err != nil {
		b, err = os.ReadFile(filepath.Join("internal", "sweatbox", "testdata", name))
	}
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func serviceAuth(t *testing.T, ts *server.TestServer) (tok string, client *http.Client) {
	t.Helper()
	tok, err := ts.MakeServiceJWT()
	if err != nil {
		t.Fatal(err)
	}
	return tok, serviceClient()
}

func doServiceHTTP(t *testing.T, ts *server.TestServer, method, path string, body []byte, contentType string) (int, []byte) {
	t.Helper()
	tok, client := serviceAuth(t, ts)
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.HTTPBaseURL()+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func loadKBTVAirport(t *testing.T, ts *server.TestServer) {
	t.Helper()
	apt := readSweatboxFixture(t, "KBTV_example.apt")
	code, body := doServiceHTTP(t, ts, http.MethodPost, "/sweatbox/airport", apt, "text/plain")
	if code != http.StatusOK {
		t.Fatalf("load airport status %d body %s", code, body)
	}
}

func loadKBTVScenario(t *testing.T, ts *server.TestServer) int {
	t.Helper()
	air := readSweatboxFixture(t, "KBTV_example.air")
	code, body := doServiceHTTP(t, ts, http.MethodPost, "/sweatbox/scenario", air, "text/plain")
	if code != http.StatusOK {
		t.Fatalf("load scenario status %d body %s", code, body)
	}
	var res serviceapi.SweatboxScenarioResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("scenario response: %v body %s", err, body)
	}
	if res.Loaded < 1 {
		t.Fatalf("expected loaded aircraft, got %+v", res)
	}
	return res.Loaded
}

func sweatboxCommand(t *testing.T, ts *server.TestServer, cmd string) serviceapi.SweatboxCommandResponse {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"command": cmd})
	code, body := doServiceHTTP(t, ts, http.MethodPost, "/sweatbox/command", payload, "application/json")
	if code != http.StatusOK && code != http.StatusConflict {
		t.Fatalf("command %q status %d body %s", cmd, code, body)
	}
	var res serviceapi.SweatboxCommandResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("command response: %v body %s", err, body)
	}
	return res
}

func sweatboxPause(t *testing.T, ts *server.TestServer) {
	t.Helper()
	code, body := doServiceHTTP(t, ts, http.MethodPost, "/sweatbox/pause", nil, "")
	if code != http.StatusNoContent {
		t.Fatalf("pause status %d body %s", code, body)
	}
}

func sweatboxUnpause(t *testing.T, ts *server.TestServer) {
	t.Helper()
	code, body := doServiceHTTP(t, ts, http.MethodPost, "/sweatbox/unpause", nil, "")
	if code != http.StatusNoContent {
		t.Fatalf("unpause status %d body %s", code, body)
	}
}

func getSweatboxState(t *testing.T, ts *server.TestServer) serviceapi.SweatboxStateJSON {
	t.Helper()
	code, body := doServiceHTTP(t, ts, http.MethodGet, "/sweatbox/state", nil, "")
	if code != http.StatusOK {
		t.Fatalf("state status %d body %s", code, body)
	}
	var st serviceapi.SweatboxStateJSON
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("state decode: %v body %s", err, body)
	}
	return st
}

func aircraftFromState(st serviceapi.SweatboxStateJSON, callsign string) (serviceapi.SweatboxAircraftJSON, bool) {
	cs := strings.ToUpper(callsign)
	for _, ac := range st.Aircraft {
		if strings.EqualFold(ac.Callsign, cs) {
			return ac, true
		}
	}
	return serviceapi.SweatboxAircraftJSON{}, false
}

func waitSweatboxAircraft(t *testing.T, ts *server.TestServer, callsign string) serviceapi.SweatboxAircraftJSON {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := getSweatboxState(t, ts)
		if ac, ok := aircraftFromState(st, callsign); ok {
			return ac
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("aircraft %s not in sweatbox state", callsign)
	return serviceapi.SweatboxAircraftJSON{}
}

func waitOnlineCallsignGone(t *testing.T, ts *server.TestServer, callsign string) {
	t.Helper()
	tok, client := serviceAuth(t, ts)
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
		var data serviceapi.OnlineUsersResponseData
		err = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		found := false
		for _, p := range data.Pilots {
			if p.Callsign == callsign {
				found = true
				break
			}
		}
		if !found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("callsign %s still in online_users", callsign)
}

func waitSweatboxAircraftGone(t *testing.T, ts *server.TestServer, callsign string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st := getSweatboxState(t, ts)
		if _, ok := aircraftFromState(st, callsign); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("aircraft %s still in sweatbox state", callsign)
}

// nearKBTVATC returns a tower position over Burlington.
func nearKBTVATC(cs string, rating protocol.NetworkRating) protocol.ATCPosition {
	return protocol.ATCPosition{
		Callsign:        cs,
		Frequencies:     "11850",
		FacilityType:    4, // TWR
		VisibilityRange: 50,
		NetworkRating:   rating,
		Latitude:        kbtvLat,
		Longitude:       kbtvLon,
	}
}

// nearKBTVPilot returns a pilot position near the field.
func nearKBTVPilot(cs string) protocol.PilotPosition {
	return protocol.PilotPosition{
		TransponderMode:  "S",
		Callsign:         cs,
		TransponderCode:  "1200",
		NetworkRating:    protocol.NetworkRatingObserver,
		Latitude:         kbtvLat,
		Longitude:        kbtvLon,
		TrueAltitude:     1000,
		Groundspeed:      0,
		PitchBankHeading: 0,
	}
}

// sendATCGeo indexes the ATC client near KBTV (predicate-poll via online_users).
func sendATCGeo(t *testing.T, ts *server.TestServer, c *fsdclient.Client, cs string, rating protocol.NetworkRating) {
	t.Helper()
	pos := nearKBTVATC(cs, rating)
	tok, client := serviceAuth(t, ts)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.SendATCPosition(pos); err != nil {
			t.Fatal(err)
		}
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
		var data serviceapi.OnlineUsersResponseData
		_ = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		for _, a := range data.ATC {
			if a.Callsign == cs && math.Abs(a.Latitude-kbtvLat) < 0.05 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ATC %s geo not indexed near KBTV", cs)
}

// collectWire watches c until pred matches both #AP and @ for callsign (or ctx ends).
// Must start before the event that emits the packets (race-safe).
func collectWire(ctx context.Context, c *fsdclient.Client, callsign string) (sawAP, sawPos bool, err error) {
	cs := []byte(callsign)
	for {
		if sawAP && sawPos {
			return sawAP, sawPos, nil
		}
		r, e := c.Next(ctx)
		if e != nil {
			return sawAP, sawPos, e
		}
		switch r.Type {
		case protocol.PacketTypeAddPilot:
			if bytes.Contains(r.Raw, cs) {
				sawAP = true
			}
		case protocol.PacketTypePilotPosition:
			if bytes.Contains(r.Raw, cs) {
				sawPos = true
			}
		}
	}
}

// ---------------------------------------------------------------------------
// E2E scenarios
// ---------------------------------------------------------------------------

// TestE2E_Sweatbox_ATCVisibility: ATC online near KBTV sees #AP and @ after scenario load.
func TestE2E_Sweatbox_ATCVisibility(t *testing.T) {
	ts := server.StartTestServer(t)

	atc := dial(t, ts)
	const atcCS = "BTV_TWR"
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)
	sendATCGeo(t, ts, atc, atcCS, protocol.NetworkRatingController1)

	const ac = "AAL123"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var sawAP, sawPos bool
	var collectErr error
	go func() {
		defer wg.Done()
		sawAP, sawPos, collectErr = collectWire(ctx, atc, ac)
	}()

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)

	wg.Wait()
	if collectErr != nil && !(sawAP && sawPos) {
		t.Fatalf("ATC did not see both #AP and @ for %s: ap=%v pos=%v err=%v (recv=%v)",
			ac, sawAP, sawPos, collectErr, atc.Recorder().Received())
	}
	if !sawAP {
		t.Fatalf("ATC missed #AP for %s (recv=%v)", ac, atc.Recorder().Received())
	}
	if !sawPos {
		t.Fatalf("ATC missed @ for %s (recv=%v)", ac, atc.Recorder().Received())
	}

	// Confirm registry / service HTTP also list the synthetic with badge field.
	p := waitOnlinePilot(t, ts, ac, sweatboxDefaultCID)
	if !p.Synthetic {
		t.Fatalf("online_users pilot %s: Synthetic=false, want true (sweatbox badge)", ac)
	}
}

// TestE2E_Sweatbox_FPQuery: ATC $CQ SERVER:FP returns plan for a synthetic aircraft.
func TestE2E_Sweatbox_FPQuery(t *testing.T) {
	ts := server.StartTestServer(t)

	atc := dial(t, ts)
	const atcCS = "FP_BTV"
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)
	_ = waitOnlinePilot(t, ts, "AAL123", sweatboxDefaultCID)

	// Drain any initial $FP broadcast so the query response is unambiguous.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	for {
		_, err := atc.Next(drainCtx)
		if err != nil {
			break
		}
	}
	drainCancel()

	if err := atc.Send([]byte("$CQ" + atcCS + ":SERVER:FP:AAL123\r\n")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := atc.WaitFor(ctx, func(r fsdclient.Received) bool {
		return r.Type == protocol.PacketTypeFlightPlan &&
			bytes.Contains(r.Raw, []byte("AAL123")) &&
			bytes.Contains(r.Raw, []byte("KBTV")) &&
			bytes.Contains(r.Raw, []byte("KBOS"))
	})
	if err != nil {
		t.Fatalf("FP query: %v (recorder=%v)", err, atc.Recorder().All())
	}
	if !bytes.Contains(got.Raw, []byte("B738")) {
		t.Fatalf("expected aircraft type in FP, got %q", got.Raw)
	}
}

// TestE2E_Sweatbox_CallsignConflict: human cannot log on with a sweatbox callsign.
func TestE2E_Sweatbox_CallsignConflict(t *testing.T) {
	ts := server.StartTestServer(t)

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)
	_ = waitOnlinePilot(t, ts, "AAL123", sweatboxDefaultCID)

	c := dial(t, ts)
	loginPilot(t, c, "AAL123", ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitError(t, c, protocol.CallsignInUseError)
}

// TestE2E_Sweatbox_PauseFreezesMotion: taxi motion then pause keeps lat/lon stable.
func TestE2E_Sweatbox_PauseFreezesMotion(t *testing.T) {
	ts := server.StartTestServer(t)

	loadKBTVAirport(t, ts)

	// Park GA9 then taxi toward runway via J.
	res := sweatboxCommand(t, ts, "add v s p @GA9")
	if !res.OK {
		t.Fatalf("add: %+v", res)
	}
	st := getSweatboxState(t, ts)
	if len(st.Aircraft) < 1 {
		t.Fatal("expected aircraft after add")
	}
	cs := st.Aircraft[0].Callsign

	res = sweatboxCommand(t, ts, cs+" taxi J 33")
	if !res.OK {
		t.Fatalf("taxi: %+v", res)
	}

	sweatboxUnpause(t, ts)

	// Wait until position moves from the parking snap.
	base := waitSweatboxAircraft(t, ts, cs)
	deadline := time.Now().Add(12 * time.Second)
	var moved serviceapi.SweatboxAircraftJSON
	for time.Now().Before(deadline) {
		ac := waitSweatboxAircraft(t, ts, cs)
		if math.Abs(ac.Lat-base.Lat) > 1e-5 || math.Abs(ac.Lon-base.Lon) > 1e-5 {
			moved = ac
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if moved.Callsign == "" {
		t.Fatalf("expected motion after unpause; base lat/lon=%.6f,%.6f", base.Lat, base.Lon)
	}

	sweatboxPause(t, ts)
	// Allow one in-flight tick to settle, then sample freeze window.
	time.Sleep(200 * time.Millisecond)
	frozen := waitSweatboxAircraft(t, ts, cs)

	// While paused, positions must remain stable across several host ticks (~1s each).
	stableUntil := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(stableUntil) {
		ac := waitSweatboxAircraft(t, ts, cs)
		if math.Abs(ac.Lat-frozen.Lat) > 1e-7 || math.Abs(ac.Lon-frozen.Lon) > 1e-7 {
			t.Fatalf("position moved while paused: was %.8f,%.8f now %.8f,%.8f",
				frozen.Lat, frozen.Lon, ac.Lat, ac.Lon)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestE2E_Sweatbox_KickLifecycle: service HTTP kick removes registry + engine aircraft.
func TestE2E_Sweatbox_KickLifecycle(t *testing.T) {
	ts := server.StartTestServer(t)

	atc := dial(t, ts)
	const atcCS = "KICK_TWR"
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)
	const ac = "AAL123"
	_ = waitOnlinePilot(t, ts, ac, sweatboxDefaultCID)

	// Watch for #DP from kick path.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var sawDP bool
	var dpErr error
	go func() {
		defer wg.Done()
		_, dpErr = atc.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypeDeletePilot && bytes.Contains(r.Raw, []byte(ac))
		})
		sawDP = dpErr == nil
	}()

	tok, client := serviceAuth(t, ts)
	kickBody := strings.NewReader(fmt.Sprintf(`{"callsign":"%s"}`, ac))
	req, err := http.NewRequest(http.MethodPost, ts.HTTPBaseURL()+"/kick_user", kickBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("kick status %d body %s", resp.StatusCode, body)
	}

	waitOnlineCallsignGone(t, ts, ac)
	waitSweatboxAircraftGone(t, ts, ac)

	wg.Wait()
	if !sawDP {
		t.Fatalf("ATC did not receive #DP for %s: %v (recv=%v)", ac, dpErr, atc.Recorder().Received())
	}
}

// TestE2E_Sweatbox_KillLifecycle: supervisor $!! removes synthetic from registry + engine.
func TestE2E_Sweatbox_KillLifecycle(t *testing.T) {
	ts := server.StartTestServer(t)

	sup := dial(t, ts)
	const supCS = "E2E_SUP2"
	loginATC(t, sup, supCS, ts.SupCID, ts.SupPassword, protocol.NetworkRatingSupervisor)
	waitMOTD(t, sup, supCS)

	// Peer ATC for #DP observation (supervisor also receives it via broadcastAll).
	atc := dial(t, ts)
	const atcCS = "KILL_TWR"
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)
	const ac = "USA456"
	_ = waitOnlinePilot(t, ts, ac, sweatboxDefaultCID)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var sawDP bool
	var dpErr error
	go func() {
		defer wg.Done()
		_, dpErr = atc.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypeDeletePilot && bytes.Contains(r.Raw, []byte(ac))
		})
		sawDP = dpErr == nil
	}()

	if err := sup.Send([]byte("$!!" + supCS + ":" + ac + ":e2e sweatbox kill")); err != nil {
		t.Fatal(err)
	}

	waitOnlineCallsignGone(t, ts, ac)
	waitSweatboxAircraftGone(t, ts, ac)

	wg.Wait()
	if !sawDP {
		t.Fatalf("ATC did not receive #DP for %s: %v (recv=%v)", ac, dpErr, atc.Recorder().Received())
	}
}

// TestE2E_Sweatbox_HTTPDelete: DELETE /sweatbox/aircraft/:cs removes synthetic + #DP.
func TestE2E_Sweatbox_HTTPDelete(t *testing.T) {
	ts := server.StartTestServer(t)

	atc := dial(t, ts)
	const atcCS = "DEL_TWR"
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)
	const ac = "N4729H"
	_ = waitOnlinePilot(t, ts, ac, sweatboxDefaultCID)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var sawDP bool
	var dpErr error
	go func() {
		defer wg.Done()
		_, dpErr = atc.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypeDeletePilot && bytes.Contains(r.Raw, []byte(ac))
		})
		sawDP = dpErr == nil
	}()

	code, body := doServiceHTTP(t, ts, http.MethodDelete, "/sweatbox/aircraft/"+ac, nil, "")
	if code != http.StatusNoContent {
		t.Fatalf("DELETE status %d body %s", code, body)
	}

	waitOnlineCallsignGone(t, ts, ac)
	waitSweatboxAircraftGone(t, ts, ac)

	wg.Wait()
	if !sawDP {
		t.Fatalf("ATC did not receive #DP for %s: %v", ac, dpErr)
	}
}

// TestE2E_Sweatbox_HumanPilotRangedPos: human pilot in range receives sweatbox @.
func TestE2E_Sweatbox_HumanPilotRangedPos(t *testing.T) {
	ts := server.StartTestServer(t)

	pilot := dial(t, ts)
	const pilotCS = "NEAR_SB"
	loginPilot(t, pilot, pilotCS, ts.PilotCID, ts.PilotPassword, protocol.NetworkRatingObserver)
	waitMOTD(t, pilot, pilotCS)

	// Index pilot geo near KBTV before synthetics register (so @ is in range).
	tok, client := serviceAuth(t, ts)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := pilot.SendPilotPosition(nearKBTVPilot(pilotCS)); err != nil {
			t.Fatal(err)
		}
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
		var data serviceapi.OnlineUsersResponseData
		_ = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		for _, p := range data.Pilots {
			if p.Callsign == pilotCS && math.Abs(p.Latitude-kbtvLat) < 0.05 {
				goto pilotIndexed
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pilot geo not indexed near KBTV")
pilotIndexed:

	const ac = "AAL123"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var sawPos bool
	var collectErr error
	go func() {
		defer wg.Done()
		_, collectErr = pilot.WaitFor(ctx, func(r fsdclient.Received) bool {
			return r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte(ac))
		})
		sawPos = collectErr == nil
	}()

	loadKBTVAirport(t, ts)
	loadKBTVScenario(t, ts)

	wg.Wait()
	if !sawPos {
		t.Fatalf("pilot did not receive ranged @ for %s: %v (recv=%v)",
			ac, collectErr, pilot.Recorder().Received())
	}
}

// TestE2E_Sweatbox_TaxiMotion: unpaused taxi changes lat/lon over wall clock.
func TestE2E_Sweatbox_TaxiMotion(t *testing.T) {
	ts := server.StartTestServer(t)

	loadKBTVAirport(t, ts)
	res := sweatboxCommand(t, ts, "add v s p @GA9")
	if !res.OK {
		t.Fatalf("add: %+v", res)
	}
	st := getSweatboxState(t, ts)
	if len(st.Aircraft) < 1 {
		t.Fatal("no aircraft")
	}
	cs := st.Aircraft[0].Callsign
	start := st.Aircraft[0]

	res = sweatboxCommand(t, ts, cs+" taxi J 33")
	if !res.OK {
		t.Fatalf("taxi: %+v", res)
	}
	// Scenario/command leaves engine paused by default for add? add does not pause,
	// but NewEngine starts paused — unpause required.
	sweatboxUnpause(t, ts)

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		ac := waitSweatboxAircraft(t, ts, cs)
		dLat := math.Abs(ac.Lat - start.Lat)
		dLon := math.Abs(ac.Lon - start.Lon)
		if dLat > 1e-5 || dLon > 1e-5 {
			// Also observe a wire @ if an ATC is listening — not required for smoke.
			t.Logf("taxi motion %s: start=%.6f,%.6f now=%.6f,%.6f Δ=%.6f,%.6f",
				cs, start.Lat, start.Lon, ac.Lat, ac.Lon, dLat, dLon)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("taxi did not move %s within timeout (start %.6f,%.6f)", cs, start.Lat, start.Lon)
}

// TestE2E_Sweatbox_LateJoinATC: ATC joining after synthetics exist eventually sees @
// (no historical #AP — same as human late-join semantics).
//
// Order matters for durability: register synthetic while paused, late ATC logs
// on + geo, arm WaitFor for @, then taxi+unpause. Unpausing before the observer
// is online burns the finite taxi broadcast window against dial ReadTimeout.
func TestE2E_Sweatbox_LateJoinATC(t *testing.T) {
	ts := server.StartTestServer(t)

	// Existing synthetics first (engine stays paused — no @ after initial register).
	loadKBTVAirport(t, ts)
	res := sweatboxCommand(t, ts, "add v s p @GA9")
	if !res.OK {
		t.Fatalf("add: %+v", res)
	}
	st := getSweatboxState(t, ts)
	if len(st.Aircraft) < 1 {
		t.Fatal("no aircraft")
	}
	cs := st.Aircraft[0].Callsign
	_ = waitOnlinePilot(t, ts, cs, sweatboxDefaultCID)

	// Late ATC joins after #AP window (synthetic already online).
	atc := dial(t, ts)
	const atcCS = "LATE_TWR"
	loginATC(t, atc, atcCS, ts.ATCCID, ts.ATCPassword, protocol.NetworkRatingController1)
	waitMOTD(t, atc, atcCS)
	sendATCGeo(t, ts, atc, atcCS, protocol.NetworkRatingController1)

	// Arm collector before motion so every taxi tick @ is observed.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	var sawAP, sawPos bool
	var collectErr error
	go func() {
		defer wg.Done()
		for {
			if sawPos {
				return
			}
			r, err := atc.Next(ctx)
			if err != nil {
				collectErr = err
				return
			}
			if r.Type == protocol.PacketTypeAddPilot && bytes.Contains(r.Raw, []byte(cs)) {
				sawAP = true
			}
			if r.Type == protocol.PacketTypePilotPosition && bytes.Contains(r.Raw, []byte(cs)) {
				sawPos = true
				return
			}
		}
	}()

	// Now start motion so subsequent ticks rebroadcast @ for the late joiner.
	res = sweatboxCommand(t, ts, cs+" taxi J 33")
	if !res.OK {
		cancel()
		wg.Wait()
		t.Fatalf("taxi: %+v", res)
	}
	sweatboxUnpause(t, ts)

	wg.Wait()
	if !sawPos {
		t.Fatalf("late ATC never received @ for %s: ap=%v err=%v (recv=%v)",
			cs, sawAP, collectErr, atc.Recorder().Received())
	}
	// Historical #AP is not required (no replay of add). Soft log only.
	if sawAP {
		t.Logf("note: late ATC also saw #AP for %s (not required)", cs)
	}
}

// TestE2E_Sweatbox_PatternEntrySmoke: enter left downwind via command; status
// surfaces on /sweatbox/state and positions update after unpause.
func TestE2E_Sweatbox_PatternEntrySmoke(t *testing.T) {
	ts := server.StartTestServer(t)
	loadKBTVAirport(t, ts)

	res := sweatboxCommand(t, ts, "add v s p 33 8")
	if !res.OK {
		t.Fatalf("add approach: %+v", res)
	}
	st := getSweatboxState(t, ts)
	if len(st.Aircraft) < 1 {
		t.Fatal("no aircraft")
	}
	cs := st.Aircraft[0].Callsign

	res = sweatboxCommand(t, ts, cs+" eld 33")
	if !res.OK {
		t.Fatalf("eld: %+v", res)
	}
	ac := waitSweatboxAircraft(t, ts, cs)
	if ac.Status != "Downwind" {
		t.Fatalf("status after eld = %q, want Downwind", ac.Status)
	}
	if !strings.Contains(ac.Instruction, "Downwind") {
		t.Errorf("instruction = %q", ac.Instruction)
	}
	startLat, startLon := ac.Lat, ac.Lon

	// Landing type + unpause: aircraft should move along the downwind.
	res = sweatboxCommand(t, ts, cs+" tg")
	if !res.OK {
		t.Fatalf("tg: %+v", res)
	}
	sweatboxUnpause(t, ts)

	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		ac = waitSweatboxAircraft(t, ts, cs)
		if math.Abs(ac.Lat-startLat) > 1e-5 || math.Abs(ac.Lon-startLon) > 1e-5 {
			t.Logf("pattern motion %s: status=%s Δlat=%.6f Δlon=%.6f",
				cs, ac.Status, ac.Lat-startLat, ac.Lon-startLon)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pattern aircraft did not move (status=%s)", waitSweatboxAircraft(t, ts, cs).Status)
}
