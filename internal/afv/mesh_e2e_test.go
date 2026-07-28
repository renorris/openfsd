package afv_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/afv"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/pkg/afvprotocol"
	"github.com/renorris/openfsd/pkg/protocol"
)

// Mesh e2e Cases A–F (MemoryMesh, protocol-level clients).

type meshNode struct {
	srv    *afv.Server
	mesh   *afv.MemoryMesh
	api    string
	udp    string
	cancel context.CancelFunc
}

func meshAuth(t *testing.T, api string, cid int) string {
	t.Helper()
	body := []byte(`{"username":"` + strconv.Itoa(cid) + `","password":"pw","client":"mesh"}`)
	resp, err := http.Post(api+"/api/v1/auth", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("auth cid=%d status=%d %s", cid, resp.StatusCode, b)
	}
	return string(b)
}

func meshPostCS(t *testing.T, api, token string, cid int, cs, udpAdvertise string) afv.PostCallsignResponse {
	t.Helper()
	path := api + "/api/v1/users/" + strconv.Itoa(cid) + "/callsigns/" + cs
	req, _ := http.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var pc afv.PostCallsignResponse
	if err := json.NewDecoder(resp.Body).Decode(&pc); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("post cs %d", resp.StatusCode)
	}
	pc.VoiceServer.AddressIpV4 = udpAdvertise
	return pc
}

func meshPostTrx(t *testing.T, api, token string, cid int, cs string, lat, lon float64, freq uint32) {
	t.Helper()
	path := api + "/api/v1/users/" + strconv.Itoa(cid) + "/callsigns/" + cs + "/transceivers"
	body, _ := json.Marshal([]afv.Transceiver{{
		ID: 0, Frequency: freq, LatDeg: lat, LonDeg: lon, HeightMslM: 100, HeightAglM: 100,
	}})
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("trx %d", resp.StatusCode)
	}
}

func meshBindHB(t *testing.T, ch *afvprotocol.Channel, cli *net.UDPConn, server *net.UDPAddr, callsign string, seq uint64) {
	t.Helper()
	hb, err := ch.Encapsulate(seq, afvprotocol.DTONameHeartbeat, afvprotocol.Heartbeat{Callsign: callsign}.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.WriteTo(hb, server); err != nil {
		t.Fatal(err)
	}
	_ = cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8192)
	n, _, err := cli.ReadFrom(buf)
	if err != nil {
		t.Fatalf("HA: %v", err)
	}
	_, name, _, err := ch.Decapsulate(buf[:n])
	if err != nil || name != "HA" {
		t.Fatalf("name=%s err=%v", name, err)
	}
}

// waitPeerWantsTX waits until local mesh believes peer wants the TX radio cell
// used by EnqueueAudioRelay fan-out (CellKey of speaker TX lat/lon), not RX pos.
func waitPeerWantsTX(t *testing.T, m *afv.MemoryMesh, peer string, freq uint32, txLat, txLon float64) {
	t.Helper()
	ck := geo.CellKey{
		ILat: geo.CellIndex(txLat, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(txLon, geo.DefaultGridCellDeg),
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if m.PeerWants(peer, freq, ck) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout PeerWants peer=%s freq=%d txCell=%+v (tx=%.4f,%.4f)", peer, freq, ck, txLat, txLon)
}

func setupMeshPairWithUsers(t *testing.T) (n1, n2 *meshNode, cidA, cidB int) {
	t.Helper()
	hub := afv.NewMemoryHub()
	m1, err := afv.NewMemoryMesh(hub, afv.MeshConfig{NodeID: "n1", PSK: "psk", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := afv.NewMemoryMesh(hub, afv.MeshConfig{NodeID: "n2", PSK: "psk", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}

	openNode := func(name string, mesh *afv.MemoryMesh) (*meshNode, *db.Repositories) {
		sqlDB, err := sql.Open("sqlite", "file:meshe2e_"+t.Name()+"_"+name+"?mode=memory&cache=shared")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err := db.Migrate(sqlDB); err != nil {
			t.Fatal(err)
		}
		repos, err := db.NewRepositories(sqlDB)
		if err != nil {
			t.Fatal(err)
		}
		cfg := &afv.Config{
			APIListen: "127.0.0.1:0", UDPListen: "127.0.0.1:0",
			UDPAdvertiseIPv4: "127.0.0.1:0", JWTTTL: time.Hour,
			MaxSessions: 100, MaxSessionsPerCID: 5,
			AuthFailMax: 50, AuthFailWindow: time.Minute, MaxDatagram: 8192,
			RangeDefaultNM: 40, RangeATCNM: 150, RangeUnicomNM: 15, RangeEdgeRatio: 0.1,
			HeartbeatTimeout: 30 * time.Second, SessionIdleTimeout: 60 * time.Second,
		}
		s := afv.New(cfg, repos.UserRepo, repos.ConfigRepo, []byte("mesh-e2e-jwt-secret-key-32b!!!!"))
		s.SetMesh(mesh)
		api, udp, cancel := startAFV(t, s)
		cfg.UDPAdvertiseIPv4 = udp
		return &meshNode{srv: s, mesh: mesh, api: api, udp: udp, cancel: cancel}, repos
	}

	var repos1, repos2 *db.Repositories
	n1, repos1 = openNode("n1", m1)
	n2, repos2 = openNode("n2", m2)
	t.Cleanup(func() { n1.cancel(); n2.cancel() })

	// Same password users; CIDs may differ per DB — create one user each
	uA := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	uB := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	if err := repos1.UserRepo.CreateUser(context.Background(), uA); err != nil {
		t.Fatal(err)
	}
	if err := repos2.UserRepo.CreateUser(context.Background(), uB); err != nil {
		t.Fatal(err)
	}
	cidA, cidB = uA.CID, uB.CID
	return n1, n2, cidA, cidB
}

func readAR(t *testing.T, ch *afvprotocol.Channel, cli *net.UDPConn, timeout time.Duration) (afvprotocol.AudioRx, bool) {
	t.Helper()
	_ = cli.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 8192)
	n, _, err := cli.ReadFrom(buf)
	if err != nil {
		return afvprotocol.AudioRx{}, false
	}
	_, name, payload, err := ch.Decapsulate(buf[:n])
	if err != nil || name != "AR" {
		return afvprotocol.AudioRx{}, false
	}
	ar, err := afvprotocol.DecodeAudioRx(payload)
	if err != nil {
		t.Fatal(err)
	}
	return ar, true
}

func TestMeshE2E_CaseA_BidirectionalA2A(t *testing.T) {
	n1, n2, cidA, cidB := setupMeshPairWithUsers(t)
	const freq uint32 = 122800000 // UNICOM
	lat, lon := 40.64, -73.78

	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	pcA := meshPostCS(t, n1.api, tokA, cidA, "AAL1", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "AAL2", n2.udp)
	meshPostTrx(t, n1.api, tokA, cidA, "AAL1", lat, lon, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "AAL2", lat+0.001, lon+0.001, freq)

	udp1, _ := net.ResolveUDPAddr("udp", n1.udp)
	udp2, _ := net.ResolveUDPAddr("udp", n2.udp)
	cliA, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer cliA.Close()
	cliB, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer cliB.Close()

	chA, err := afvprotocol.ClientChannel(pcA.VoiceServer.ChannelConfig.ChannelTag, pcA.VoiceServer.ChannelConfig.AeadReceiveKey, pcA.VoiceServer.ChannelConfig.AeadTransmitKey)
	if err != nil {
		t.Fatal(err)
	}
	chB, err := afvprotocol.ClientChannel(pcB.VoiceServer.ChannelConfig.ChannelTag, pcB.VoiceServer.ChannelConfig.AeadReceiveKey, pcB.VoiceServer.ChannelConfig.AeadTransmitKey)
	if err != nil {
		t.Fatal(err)
	}

	meshBindHB(t, chA, cliA, udp1, "AAL1", 0)
	meshBindHB(t, chB, cliB, udp2, "AAL2", 0)

	// force interest now; barriers use speaker TX cells (fan-out filter keys)
	n1.srv.PublishInterestNowForTest()
	n2.srv.PublishInterestNowForTest()
	// A→B: n1 fans out if n2 wants A's TX cell
	waitPeerWantsTX(t, n1.mesh, "n2", freq, lat, lon)
	// B→A: n2 fans out if n1 wants B's TX cell
	waitPeerWantsTX(t, n2.mesh, "n1", freq, lat+0.001, lon+0.001)

	// A → B
	at := afvprotocol.AudioTx{
		Callsign: "AAL1", SequenceCounter: 7, Audio: []byte{1, 2, 3, 4}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, err := chA.Encapsulate(1, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cliA.WriteTo(pkt, udp1); err != nil {
		t.Fatal(err)
	}
	ar, ok := readAR(t, chB, cliB, 3*time.Second)
	if !ok {
		t.Fatal("Case A: B did not receive AR from A")
	}
	if ar.Callsign != "AAL1" || !bytes.Equal(ar.Audio, at.Audio) || ar.SequenceCounter != 7 || !ar.LastPacket {
		t.Fatalf("AR=%+v", ar)
	}

	// B → A
	at2 := afvprotocol.AudioTx{
		Callsign: "AAL2", SequenceCounter: 9, Audio: []byte{9, 8, 7}, LastPacket: false,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt2, err := chB.Encapsulate(1, afvprotocol.DTONameAudioTx, at2.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cliB.WriteTo(pkt2, udp2); err != nil {
		t.Fatal(err)
	}
	ar2, ok := readAR(t, chA, cliA, 3*time.Second)
	if !ok {
		t.Fatal("Case A: A did not receive AR from B")
	}
	if ar2.Callsign != "AAL2" || ar2.SequenceCounter != 9 || ar2.LastPacket {
		t.Fatalf("AR2=%+v want LastPacket=false", ar2)
	}
}

func TestMeshE2E_CaseB_OutOfRange(t *testing.T) {
	n1, n2, cidA, cidB := setupMeshPairWithUsers(t)
	const freq uint32 = 122800000 // UNICOM 15NM
	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	pcA := meshPostCS(t, n1.api, tokA, cidA, "AAL1", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "AAL2", n2.udp)
	// ~1 deg lat ≈ 60NM >> 15NM UNICOM
	meshPostTrx(t, n1.api, tokA, cidA, "AAL1", 40.0, -73.0, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "AAL2", 41.0, -73.0, freq)

	udp1, _ := net.ResolveUDPAddr("udp", n1.udp)
	udp2, _ := net.ResolveUDPAddr("udp", n2.udp)
	cliA, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliA.Close()
	cliB, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliB.Close()
	chA, _ := afvprotocol.ClientChannel(pcA.VoiceServer.ChannelConfig.ChannelTag, pcA.VoiceServer.ChannelConfig.AeadReceiveKey, pcA.VoiceServer.ChannelConfig.AeadTransmitKey)
	chB, _ := afvprotocol.ClientChannel(pcB.VoiceServer.ChannelConfig.ChannelTag, pcB.VoiceServer.ChannelConfig.AeadReceiveKey, pcB.VoiceServer.ChannelConfig.AeadTransmitKey)
	meshBindHB(t, chA, cliA, udp1, "AAL1", 0)
	meshBindHB(t, chB, cliB, udp2, "AAL2", 0)
	n1.srv.PublishInterestNowForTest()
	n2.srv.PublishInterestNowForTest()
	// interest may still cover (UNICOM interest is 15NM) — TX cell of peer may not be wanted
	// Even if mesh relays, local range should drop.
	// Force interest to include TX cells so we test range not interest filter
	n1.mesh.ApplyInterestDirect("n2", []afv.InterestEntry{{
		FreqHz: freq,
		ILat:   geo.CellIndex(40.0, geo.DefaultGridCellDeg),
		ILon:   geo.CellIndex(-73.0, geo.DefaultGridCellDeg),
	}})
	// wait a tick
	time.Sleep(50 * time.Millisecond)

	at := afvprotocol.AudioTx{
		Callsign: "AAL1", SequenceCounter: 1, Audio: []byte{1}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, _ := chA.Encapsulate(1, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	_, _ = cliA.WriteTo(pkt, udp1)
	if _, ok := readAR(t, chB, cliB, 400*time.Millisecond); ok {
		t.Fatal("Case B: unexpected AR out of range")
	}
}

func TestMeshE2E_CaseC_ATCRadius(t *testing.T) {
	n1, n2, cidA, cidB := setupMeshPairWithUsers(t)
	const freq uint32 = 118700000 // non-UNICOM
	// ~100 NM: 1.5 deg lat
	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	// ATC callsign with underscore on n1
	pcA := meshPostCS(t, n1.api, tokA, cidA, "JFK_TWR", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "AAL2", n2.udp)
	meshPostTrx(t, n1.api, tokA, cidA, "JFK_TWR", 40.0, -73.0, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "AAL2", 41.5, -73.0, freq)

	udp1, _ := net.ResolveUDPAddr("udp", n1.udp)
	udp2, _ := net.ResolveUDPAddr("udp", n2.udp)
	cliA, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliA.Close()
	cliB, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliB.Close()
	chA, _ := afvprotocol.ClientChannel(pcA.VoiceServer.ChannelConfig.ChannelTag, pcA.VoiceServer.ChannelConfig.AeadReceiveKey, pcA.VoiceServer.ChannelConfig.AeadTransmitKey)
	chB, _ := afvprotocol.ClientChannel(pcB.VoiceServer.ChannelConfig.ChannelTag, pcB.VoiceServer.ChannelConfig.AeadReceiveKey, pcB.VoiceServer.ChannelConfig.AeadTransmitKey)
	meshBindHB(t, chA, cliA, udp1, "JFK_TWR", 0)
	meshBindHB(t, chB, cliB, udp2, "AAL2", 0)
	n1.srv.PublishInterestNowForTest()
	n2.srv.PublishInterestNowForTest()
	// ATC TX at 40.0: n1 fans out if n2 wants that TX cell (ATC-range interest)
	waitPeerWantsTX(t, n1.mesh, "n2", freq, 40.0, -73.0)

	// ATC TX → pilot AR
	at := afvprotocol.AudioTx{
		Callsign: "JFK_TWR", SequenceCounter: 1, Audio: []byte{5, 5}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, _ := chA.Encapsulate(1, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	_, _ = cliA.WriteTo(pkt, udp1)
	if _, ok := readAR(t, chB, cliB, 3*time.Second); !ok {
		t.Fatal("Case C: pilot should hear ATC at ~100NM")
	}

	// pilot TX same distance → no AR for ATC (Default 40NM)
	// drain any leftover
	_, _ = readAR(t, chA, cliA, 50*time.Millisecond)
	n2.srv.PublishInterestNowForTest()
	// pilot TX at 41.5: n2 fans out if n1 wants pilot TX cell
	waitPeerWantsTX(t, n2.mesh, "n1", freq, 41.5, -73.0)
	atP := afvprotocol.AudioTx{
		Callsign: "AAL2", SequenceCounter: 2, Audio: []byte{6}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pktP, _ := chB.Encapsulate(2, afvprotocol.DTONameAudioTx, atP.EncodeMsgpack(), nil)
	_, _ = cliB.WriteTo(pktP, udp2)
	if _, ok := readAR(t, chA, cliA, 400*time.Millisecond); ok {
		t.Fatal("Case C: ATC should not hear pilot at ~100NM under Default range")
	}
}

func TestMeshE2E_CaseD_PeerDeathReconnect(t *testing.T) {
	n1, n2, cidA, cidB := setupMeshPairWithUsers(t)
	const freq uint32 = 122800000
	lat, lon := 40.64, -73.78
	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	pcA := meshPostCS(t, n1.api, tokA, cidA, "AAL1", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "AAL2", n2.udp)
	meshPostTrx(t, n1.api, tokA, cidA, "AAL1", lat, lon, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "AAL2", lat+0.001, lon+0.001, freq)

	udp1, _ := net.ResolveUDPAddr("udp", n1.udp)
	udp2, _ := net.ResolveUDPAddr("udp", n2.udp)
	cliA, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliA.Close()
	cliB, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliB.Close()
	chA, _ := afvprotocol.ClientChannel(pcA.VoiceServer.ChannelConfig.ChannelTag, pcA.VoiceServer.ChannelConfig.AeadReceiveKey, pcA.VoiceServer.ChannelConfig.AeadTransmitKey)
	chB, _ := afvprotocol.ClientChannel(pcB.VoiceServer.ChannelConfig.ChannelTag, pcB.VoiceServer.ChannelConfig.AeadReceiveKey, pcB.VoiceServer.ChannelConfig.AeadTransmitKey)
	meshBindHB(t, chA, cliA, udp1, "AAL1", 0)
	meshBindHB(t, chB, cliB, udp2, "AAL2", 0)
	n1.srv.PublishInterestNowForTest()
	n2.srv.PublishInterestNowForTest()
	waitPeerWantsTX(t, n1.mesh, "n2", freq, lat, lon)

	// peer death both directions
	n1.mesh.SimulatePeerDown("n2")
	n2.mesh.SimulatePeerDown("n1")
	// remote dir purged
	if n1.srv.RemoteDir() != nil && n1.srv.RemoteDir().CountOrigin("n2") != 0 {
		t.Fatal("remote dir should purge")
	}
	// local HA still works
	meshBindHB(t, chA, cliA, udp1, "AAL1", 10)
	// no cross-node AR
	at := afvprotocol.AudioTx{
		Callsign: "AAL1", SequenceCounter: 3, Audio: []byte{1}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, _ := chA.Encapsulate(11, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	_, _ = cliA.WriteTo(pkt, udp1)
	if _, ok := readAR(t, chB, cliB, 300*time.Millisecond); ok {
		t.Fatal("AR during peer death")
	}

	// reconnect: SimulatePeerUp re-runs Snapshot + current Interest via provider (M-16)
	n1.mesh.SimulatePeerUp("n2")
	n2.mesh.SimulatePeerUp("n1")
	// No PublishInterestNowForTest — rely on SetInterestProvider path from Server.
	waitPeerWantsTX(t, n1.mesh, "n2", freq, lat, lon)
	waitPeerWantsTX(t, n2.mesh, "n1", freq, lat+0.001, lon+0.001)

	at.SequenceCounter = 4
	pkt, _ = chA.Encapsulate(12, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	_, _ = cliA.WriteTo(pkt, udp1)
	if _, ok := readAR(t, chB, cliB, 3*time.Second); !ok {
		t.Fatal("Case D: AR after reconnect")
	}
}

func TestMeshE2E_CaseE_KeysNeverOnMesh(t *testing.T) {
	n1, n2, cidA, cidB := setupMeshPairWithUsers(t)
	const freq uint32 = 122800000
	lat, lon := 40.64, -73.78
	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	pcA := meshPostCS(t, n1.api, tokA, cidA, "AAL1", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "AAL2", n2.udp)
	meshPostTrx(t, n1.api, tokA, cidA, "AAL1", lat, lon, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "AAL2", lat+0.001, lon+0.001, freq)

	udp1, _ := net.ResolveUDPAddr("udp", n1.udp)
	udp2, _ := net.ResolveUDPAddr("udp", n2.udp)
	cliA, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliA.Close()
	cliB, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliB.Close()
	chA, _ := afvprotocol.ClientChannel(pcA.VoiceServer.ChannelConfig.ChannelTag, pcA.VoiceServer.ChannelConfig.AeadReceiveKey, pcA.VoiceServer.ChannelConfig.AeadTransmitKey)
	chB, _ := afvprotocol.ClientChannel(pcB.VoiceServer.ChannelConfig.ChannelTag, pcB.VoiceServer.ChannelConfig.AeadReceiveKey, pcB.VoiceServer.ChannelConfig.AeadTransmitKey)
	meshBindHB(t, chA, cliA, udp1, "AAL1", 0)
	meshBindHB(t, chB, cliB, udp2, "AAL2", 0)
	n1.srv.PublishInterestNowForTest()
	n2.srv.PublishInterestNowForTest()
	waitPeerWantsTX(t, n1.mesh, "n2", freq, lat, lon)

	rxKey := append([]byte(nil), pcA.VoiceServer.ChannelConfig.AeadReceiveKey...)
	txKey := append([]byte(nil), pcA.VoiceServer.ChannelConfig.AeadTransmitKey...)

	at := afvprotocol.AudioTx{
		Callsign: "AAL1", SequenceCounter: 1, Audio: []byte{1, 2, 3}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, _ := chA.Encapsulate(1, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	_, _ = cliA.WriteTo(pkt, udp1)
	// wait for enqueue capture
	deadline := time.Now().Add(2 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		for _, r := range n1.mesh.LastRelays() {
			enc, err := afv.EncodeAudioRelay(r)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(enc, rxKey) || bytes.Contains(enc, txKey) {
				t.Fatal("Case E: AEAD key material found on mesh AudioRelay")
			}
			found = true
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("no relay captured")
	}
}

func TestMeshE2E_CaseF_EmptyInterestNoFlood(t *testing.T) {
	n1, n2, cidA, cidB := setupMeshPairWithUsers(t)
	const freq uint32 = 122800000
	lat, lon := 40.64, -73.78
	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	pcA := meshPostCS(t, n1.api, tokA, cidA, "AAL1", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "AAL2", n2.udp)
	meshPostTrx(t, n1.api, tokA, cidA, "AAL1", lat, lon, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "AAL2", lat+0.001, lon+0.001, freq)
	udp1, _ := net.ResolveUDPAddr("udp", n1.udp)
	udp2, _ := net.ResolveUDPAddr("udp", n2.udp)
	cliA, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliA.Close()
	cliB, _ := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	defer cliB.Close()
	chA, _ := afvprotocol.ClientChannel(pcA.VoiceServer.ChannelConfig.ChannelTag, pcA.VoiceServer.ChannelConfig.AeadReceiveKey, pcA.VoiceServer.ChannelConfig.AeadTransmitKey)
	chB, _ := afvprotocol.ClientChannel(pcB.VoiceServer.ChannelConfig.ChannelTag, pcB.VoiceServer.ChannelConfig.AeadReceiveKey, pcB.VoiceServer.ChannelConfig.AeadTransmitKey)
	meshBindHB(t, chA, cliA, udp1, "AAL1", 0)
	meshBindHB(t, chB, cliB, udp2, "AAL2", 0)

	// Explicit empty Interest from n2; clear dirty so interest loop cannot
	// immediately republish full RX coverage (would race the no-flood assert).
	ck := geo.CellKey{
		ILat: geo.CellIndex(lat+0.001, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(lon+0.001, geo.DefaultGridCellDeg),
	}
	n2.srv.ClearInterestDirtyForTest()
	n2.mesh.PublishInterest(nil)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !n1.mesh.PeerWants("n2", freq, ck) {
			break
		}
		n2.srv.ClearInterestDirtyForTest()
		n2.mesh.PublishInterest(nil)
		time.Sleep(10 * time.Millisecond)
	}
	if n1.mesh.PeerWants("n2", freq, ck) {
		t.Fatal("Case F: peer still wants after empty Interest")
	}
	// Keep dirty clear during burst
	n2.srv.ClearInterestDirtyForTest()
	n1.srv.ClearInterestDirtyForTest()

	at := afvprotocol.AudioTx{
		Callsign: "AAL1", SequenceCounter: 1, Audio: []byte{1}, LastPacket: true,
		Transceivers: []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, _ := chA.Encapsulate(1, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	for i := 0; i < 5; i++ {
		n2.srv.ClearInterestDirtyForTest()
		_, _ = cliA.WriteTo(pkt, udp1)
	}
	if _, ok := readAR(t, chB, cliB, 400*time.Millisecond); ok {
		t.Fatal("Case F: flood with empty peer interest")
	}
	if n1.mesh.PeerWants("n2", freq, ck) {
		t.Fatal("Case F: PeerWants became true during burst")
	}
}

// TestFirstBindDirtyOnceViaUDPHB exercises production BindUDP→markInterestDirty
// via real UDP heartbeat (not a reimplemented mark path).
func TestFirstBindDirtyOnceViaUDPHB(t *testing.T) {
	n1, _, cidA, _ := setupMeshPairWithUsers(t)
	tok := meshAuth(t, n1.api, cidA)
	pc := meshPostCS(t, n1.api, tok, cidA, "P1", n1.udp)
	meshPostTrx(t, n1.api, tok, cidA, "P1", 40.0, -73.0, 118700000)

	// clear dirty from trx post
	n1.srv.ClearInterestDirtyForTest()
	if n1.srv.InterestDirtyForTest() {
		t.Fatal("dirty should be clear before first HB")
	}

	udp, err := net.ResolveUDPAddr("udp", n1.udp)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ch, err := afvprotocol.ClientChannel(
		pc.VoiceServer.ChannelConfig.ChannelTag,
		pc.VoiceServer.ChannelConfig.AeadReceiveKey,
		pc.VoiceServer.ChannelConfig.AeadTransmitKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	// First HB binds and dirties interest (production udp.go path)
	meshBindHB(t, ch, cli, udp, "P1", 0)
	// allow handleUDP to finish
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if n1.srv.InterestDirtyForTest() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !n1.srv.InterestDirtyForTest() {
		t.Fatal("first UDP HB must mark interest dirty")
	}

	n1.srv.ClearInterestDirtyForTest()
	// Second HB same addr: re-touch, must not dirty again
	meshBindHB(t, ch, cli, udp, "P1", 1)
	time.Sleep(50 * time.Millisecond)
	if n1.srv.InterestDirtyForTest() {
		t.Fatal("second HB re-touch must not dirty interest")
	}
}
