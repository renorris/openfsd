package afv_test

import (
	"context"
	"database/sql"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/afv"
	"github.com/renorris/openfsd/internal/db"
	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/pkg/afvprotocol"
	"github.com/renorris/openfsd/pkg/protocol"
)

func freePortTCP(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

func freePortUDP(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()
	return p
}

type hybridNode struct {
	srv    *afv.Server
	mesh   *afv.HybridMesh
	api    string
	udp    string
	cancel context.CancelFunc
}

func setupHybridPair(t *testing.T) (n1, n2 *hybridNode, cidA, cidB int) {
	t.Helper()
	tcp1, voice1 := freePortTCP(t), freePortUDP(t)
	tcp2, voice2 := freePortTCP(t), freePortUDP(t)

	open := func(name, nodeID, peerID string, tcp, voice, peerTCP, peerVoice int) (*hybridNode, *db.Repositories) {
		sqlDB, err := sql.Open("sqlite", "file:hybride2e_"+t.Name()+"_"+name+"?mode=memory&cache=shared")
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
		hm, err := afv.NewHybridMesh(afv.HybridMeshConfig{
			NodeID:      nodeID,
			ListenTCP:   "127.0.0.1:" + strconv.Itoa(tcp),
			ListenVoice: "127.0.0.1:" + strconv.Itoa(voice),
			Peers: []afv.ClusterPeer{{
				ID: peerID, Addr: "127.0.0.1:" + strconv.Itoa(peerTCP),
				VoiceAddr: "127.0.0.1:" + strconv.Itoa(peerVoice),
			}},
			PSK: "hybrid-e2e-psk",
		})
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
		s := afv.New(cfg, repos.UserRepo, repos.ConfigRepo, []byte("hybrid-e2e-jwt-secret-key-32b!!"))
		s.SetMesh(hm)
		api, udp, cancel := startAFV(t, s)
		cfg.UDPAdvertiseIPv4 = udp
		return &hybridNode{srv: s, mesh: hm, api: api, udp: udp, cancel: cancel}, repos
	}

	var r1, r2 *db.Repositories
	n1, r1 = open("n1", "n1", "n2", tcp1, voice1, tcp2, voice2)
	n2, r2 = open("n2", "n2", "n1", tcp2, voice2, tcp1, voice1)
	t.Cleanup(func() { n1.cancel(); n2.cancel() })

	// Wait mesh auth both ways
	if !n1.mesh.WaitPeerAuthed("n2", 5*time.Second) {
		t.Fatal("n1↔n2 auth timeout")
	}
	if !n2.mesh.WaitPeerAuthed("n1", 5*time.Second) {
		t.Fatal("n2↔n1 auth timeout")
	}

	uA := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	uB := &db.User{Password: "pw", NetworkRating: int(protocol.NetworkRatingObserver), PilotRating: 1}
	if err := r1.UserRepo.CreateUser(context.Background(), uA); err != nil {
		t.Fatal(err)
	}
	if err := r2.UserRepo.CreateUser(context.Background(), uB); err != nil {
		t.Fatal(err)
	}
	return n1, n2, uA.CID, uB.CID
}

func waitHybridPeerWants(t *testing.T, m *afv.HybridMesh, peer string, freq uint32, txLat, txLon float64) {
	t.Helper()
	ck := geo.CellKey{
		ILat: geo.CellIndex(txLat, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(txLon, geo.DefaultGridCellDeg),
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if m.PeerWants(peer, freq, ck) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout PeerWants peer=%s", peer)
}

func TestHybridMeshE2E_A2A(t *testing.T) {
	n1, n2, cidA, cidB := setupHybridPair(t)

	tokA := meshAuth(t, n1.api, cidA)
	tokB := meshAuth(t, n2.api, cidB)
	pcA := meshPostCS(t, n1.api, tokA, cidA, "N1A", n1.udp)
	pcB := meshPostCS(t, n2.api, tokB, cidB, "N2B", n2.udp)

	// Same frequency, nearby positions (in range)
	freq := uint32(122800000)
	latA, lonA := 51.4700, -0.4543
	latB, lonB := 51.4710, -0.4530
	meshPostTrx(t, n1.api, tokA, cidA, "N1A", latA, lonA, freq)
	meshPostTrx(t, n2.api, tokB, cidB, "N2B", latB, lonB, freq)

	// Bind UDP + HA first — Interest only includes Bound sessions
	srvA, err := net.ResolveUDPAddr("udp", n1.udp)
	if err != nil {
		t.Fatal(err)
	}
	srvB, err := net.ResolveUDPAddr("udp", n2.udp)
	if err != nil {
		t.Fatal(err)
	}
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

	meshBindHB(t, chA, cliA, srvA, "N1A", 0)
	meshBindHB(t, chB, cliB, srvB, "N2B", 0)

	// Interest after bind; barrier on PeerWants (async TCP apply)
	n1.srv.PublishInterestNowForTest()
	n2.srv.PublishInterestNowForTest()
	waitHybridPeerWants(t, n1.mesh, "n2", freq, latA, lonA)

	// A transmits AT
	at := afvprotocol.AudioTx{
		Callsign:        "N1A",
		SequenceCounter: 1,
		Audio:           []byte("hybrid-opus"),
		LastPacket:      false,
		Transceivers:    []afvprotocol.TxTransceiver{{ID: 0}},
	}
	pkt, err := chA.Encapsulate(2, afvprotocol.DTONameAudioTx, at.EncodeMsgpack(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cliA.WriteTo(pkt, srvA); err != nil {
		t.Fatal(err)
	}

	ar, ok := readAR(t, chB, cliB, 3*time.Second)
	if !ok {
		t.Fatal("B did not receive AR via hybrid mesh")
	}
	if ar.Callsign != "N1A" {
		t.Fatalf("callsign %q", ar.Callsign)
	}
}

func TestHybridMeshE2E_WrongPSK(t *testing.T) {
	tcp1, voice1 := freePortTCP(t), freePortUDP(t)
	tcp2, voice2 := freePortTCP(t), freePortUDP(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m1, err := afv.NewHybridMesh(afv.HybridMeshConfig{
		NodeID: "n1", ListenTCP: "127.0.0.1:" + strconv.Itoa(tcp1), ListenVoice: "127.0.0.1:" + strconv.Itoa(voice1),
		Peers: []afv.ClusterPeer{{ID: "n2", Addr: "127.0.0.1:" + strconv.Itoa(tcp2), VoiceAddr: "127.0.0.1:" + strconv.Itoa(voice2)}},
		PSK:   "psk-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := afv.NewHybridMesh(afv.HybridMeshConfig{
		NodeID: "n2", ListenTCP: "127.0.0.1:" + strconv.Itoa(tcp2), ListenVoice: "127.0.0.1:" + strconv.Itoa(voice2),
		Peers: []afv.ClusterPeer{{ID: "n1", Addr: "127.0.0.1:" + strconv.Itoa(tcp1), VoiceAddr: "127.0.0.1:" + strconv.Itoa(voice1)}},
		PSK:   "psk-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m1.Stop()
	defer m2.Stop()
	// Poll window: must remain unauthenticated under wrong PSK.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m1.PeerAuthedForTest("n2") || m2.PeerAuthedForTest("n1") {
			t.Fatal("wrong PSK must not auth")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
