package afv

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func hybridPairPorts(t *testing.T) (tcp1, voice1, tcp2, voice2 int) {
	t.Helper()
	tcp1 = freeTCPPort(t)
	voice1 = freeUDPPort(t)
	tcp2 = freeTCPPort(t)
	voice2 = freeUDPPort(t)
	return
}

func startHybridPair(t *testing.T, psk string) (m1, m2 *HybridMesh, cancel context.CancelFunc) {
	t.Helper()
	tcp1, voice1, tcp2, voice2 := hybridPairPorts(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg1 := HybridMeshConfig{
		NodeID:      "n1",
		ListenTCP:   "127.0.0.1:" + itoa(tcp1),
		ListenVoice: "127.0.0.1:" + itoa(voice1),
		Peers: []ClusterPeer{{
			ID: "n2", Addr: "127.0.0.1:" + itoa(tcp2), VoiceAddr: "127.0.0.1:" + itoa(voice2),
		}},
		PSK: psk,
	}
	cfg2 := HybridMeshConfig{
		NodeID:      "n2",
		ListenTCP:   "127.0.0.1:" + itoa(tcp2),
		ListenVoice: "127.0.0.1:" + itoa(voice2),
		Peers: []ClusterPeer{{
			ID: "n1", Addr: "127.0.0.1:" + itoa(tcp1), VoiceAddr: "127.0.0.1:" + itoa(voice1),
		}},
		PSK: psk,
	}
	var err error
	m1, err = NewHybridMesh(cfg1)
	if err != nil {
		t.Fatal(err)
	}
	m2, err = NewHybridMesh(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m1.Stop(); _ = m2.Stop() })
	if !m1.WaitPeerAuthed("n2", 3*time.Second) {
		t.Fatal("n1 did not auth n2")
	}
	if !m2.WaitPeerAuthed("n1", 3*time.Second) {
		t.Fatal("n2 did not auth n1")
	}
	return m1, m2, cancel
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func geoCell(ilat, ilon int32) geo.CellKey {
	return geo.CellKey{ILat: ilat, ILon: ilon}
}

func TestHybridMesh_HelloWrongPSK(t *testing.T) {
	tcp1, voice1, tcp2, voice2 := hybridPairPorts(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m1, err := NewHybridMesh(HybridMeshConfig{
		NodeID: "n1", ListenTCP: "127.0.0.1:" + itoa(tcp1), ListenVoice: "127.0.0.1:" + itoa(voice1),
		Peers: []ClusterPeer{{ID: "n2", Addr: "127.0.0.1:" + itoa(tcp2), VoiceAddr: "127.0.0.1:" + itoa(voice2)}},
		PSK:   "secret-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := NewHybridMesh(HybridMeshConfig{
		NodeID: "n2", ListenTCP: "127.0.0.1:" + itoa(tcp2), ListenVoice: "127.0.0.1:" + itoa(voice2),
		Peers: []ClusterPeer{{ID: "n1", Addr: "127.0.0.1:" + itoa(tcp1), VoiceAddr: "127.0.0.1:" + itoa(voice1)}},
		PSK:   "secret-b",
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

	// Poll a window: must never become authed under wrong PSK.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m1.PeerAuthedForTest("n2") || m2.PeerAuthedForTest("n1") {
			t.Fatal("wrong PSK must not authenticate")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHybridMesh_NonHelloFirstFrame(t *testing.T) {
	tcpPort := freeTCPPort(t)
	voicePort := freeUDPPort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Peer that will dial us (higher ID) — but we inject a raw non-Hello first frame
	// by connecting as an attacker to the listen port.
	m, err := NewHybridMesh(HybridMeshConfig{
		NodeID: "n1", ListenTCP: "127.0.0.1:" + itoa(tcpPort), ListenVoice: "127.0.0.1:" + itoa(voicePort),
		Peers: []ClusterPeer{{ID: "n2", Addr: "127.0.0.1:1", VoiceAddr: "127.0.0.1:2"}},
		PSK:   "psk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()

	conn, err := net.Dial("tcp", "127.0.0.1:"+itoa(tcpPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Send Heartbeat as first frame
	if err := EncodeMeshFrame(conn, MeshTypeHeartbeat, EncodeHeartbeatPayload(1)); err != nil {
		t.Fatal(err)
	}
	// Server should close after rejecting non-Hello
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	_, err = conn.Read(buf)
	if err == nil {
		// may get Hello from server first, then close
		_, err = io.ReadAll(conn)
	}
	// peer must not be authed
	if m.PeerAuthedForTest("n2") {
		t.Fatal("non-Hello first frame must not auth")
	}
}

func TestHybridMesh_Type20OnTCPCloses(t *testing.T) {
	m1, m2, _ := startHybridPair(t, "psk")
	_ = m2

	// Inject type 20 on the TCP control conn from m1's peer n2
	m1.mu.RLock()
	p := m1.peers["n2"]
	m1.mu.RUnlock()
	if p == nil {
		t.Fatal("no peer")
	}
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		t.Fatal("no conn")
	}
	// Write AudioRelay frame on TCP
	pay, err := EncodeAudioRelay(AudioRelay{OriginNode: "n1", Callsign: "X", Audio: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := EncodeMeshFrame(conn, MeshTypeAudioRelay, pay); err != nil {
		t.Fatal(err)
	}
	// Wait for peer death on m2 (received type 20)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !m2.PeerAuthedForTest("n1") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("type 20 on TCP should close peer on receiver")
}

func TestHybridMesh_InterestReplace(t *testing.T) {
	m1, m2, _ := startHybridPair(t, "psk")

	// m2 publishes Interest → applied on m1's TCP reader
	entries := []InterestEntry{{FreqHz: 122800000, ILat: 1, ILon: 2}}
	m2.PublishInterest(entries)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m1.PeerWants("n2", 122800000, geoCell(1, 2)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !m1.PeerWants("n2", 122800000, geoCell(1, 2)) {
		t.Fatal("interest not applied")
	}

	// Empty Interest clears (replace, not merge)
	m2.PublishInterest(nil)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !m1.PeerWants("n2", 122800000, geoCell(1, 2)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("empty Interest should clear prior set")
}

func TestHybridMesh_LastHBAdvancesOnTrxDelta(t *testing.T) {
	// Short death grace for test speed — use normal and just verify lastHB moves
	m1, m2, _ := startHybridPair(t, "psk")

	m1.mu.RLock()
	p := m1.peers["n2"]
	m1.mu.RUnlock()
	p.mu.Lock()
	before := p.lastHB
	p.mu.Unlock()

	time.Sleep(20 * time.Millisecond)
	m2.PublishTrxDelta("N2PILOT", false, []Transceiver{{ID: 0, Frequency: 1}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		after := p.lastHB
		p.mu.Unlock()
		if after.After(before) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("lastHB should advance on TrxDelta")
}

func TestHybridMesh_PeerDeathReadError(t *testing.T) {
	var dead atomic.Bool
	m1, m2, _ := startHybridPair(t, "psk")
	m1.OnPeerDead(func(id string) {
		if id == "n2" {
			dead.Store(true)
		}
	})
	// Arm Interest then kill
	m2.PublishInterest([]InterestEntry{{FreqHz: 1, ILat: 0, ILon: 0}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m1.PeerWants("n2", 1, geo.CellKey{}) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m1.ForcePeerDeathForTest("n2")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if dead.Load() && !m1.PeerAuthedForTest("n2") && !m1.PeerWants("n2", 1, geo.CellKey{}) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("peer death: dead=%v authed=%v wants=%v", dead.Load(), m1.PeerAuthedForTest("n2"), m1.PeerWants("n2", 1, geo.CellKey{}))
}

func TestHybridMesh_CtrlQueueDropOldest(t *testing.T) {
	// No live peer writer: start only n1 so n2 never Hello — force drops.
	tcp1, voice1, tcp2, voice2 := hybridPairPorts(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m1, err := NewHybridMesh(HybridMeshConfig{
		NodeID: "n1", ListenTCP: "127.0.0.1:" + itoa(tcp1), ListenVoice: "127.0.0.1:" + itoa(voice1),
		Peers: []ClusterPeer{{ID: "n2", Addr: "127.0.0.1:" + itoa(tcp2), VoiceAddr: "127.0.0.1:" + itoa(voice2)}},
		PSK:   "psk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m1.Stop()

	before := m1.CtrlQueueDrops()
	m1.ForceEnqueueCtrlForTest("n2", meshControlQueueDepth+50)
	if m1.CtrlQueueDrops() <= before {
		t.Fatalf("want ctrl drops, got %d→%d", before, m1.CtrlQueueDrops())
	}
	if m1.PeerCtrlQueueLen("n2") > meshControlQueueDepth {
		t.Fatal("queue over depth")
	}
}

func TestHybridMesh_DualConnReplace(t *testing.T) {
	// T10b: second live Hello replaces first without OnPeerDead (soft replace).
	m1, m2, _ := startHybridPair(t, "psk")
	var deadN1 atomic.Int32
	m2.OnPeerDead(func(id string) {
		if id == "n1" {
			deadN1.Add(1)
		}
	})

	// Seed Interest on m2's view of n1 (m1 publishes → m2 applies).
	m1.PublishInterest([]InterestEntry{{FreqHz: 42, ILat: 1, ILon: 2}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m2.PeerWants("n1", 42, geoCell(1, 2)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !m2.PeerWants("n1", 42, geoCell(1, 2)) {
		t.Fatal("interest not applied before replace")
	}

	oldConn := m2.CurrentConnForTest("n1")
	if oldConn == nil {
		t.Fatal("expected live conn")
	}

	// Second TCP session claiming n1 → soft replace on m2.
	conn, err := net.Dial("tcp", m2.ListenTCPAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// m2 sends Hello first; read it, then send ours.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	fr, err := DecodeMeshFrame(conn)
	if err != nil || fr.Type != MeshTypeHello {
		t.Fatalf("expected Hello from m2: %v %+v", err, fr)
	}
	hello := EncodeHelloPayload(HelloPayload{NodeID: "n1", PSK: "psk"})
	if err := EncodeMeshFrame(conn, MeshTypeHello, hello); err != nil {
		t.Fatal(err)
	}

	// Wait until current conn is replaced and still authed.
	deadline = time.Now().Add(3 * time.Second)
	replaced := false
	for time.Now().Before(deadline) {
		cur := m2.CurrentConnForTest("n1")
		if cur != nil && cur != oldConn && m2.PeerAuthedForTest("n1") {
			replaced = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !replaced {
		t.Fatal("second Hello did not replace peer conn")
	}
	if deadN1.Load() != 0 {
		t.Fatalf("OnPeerDead must not fire on soft replace, got %d", deadN1.Load())
	}
	// Soft replace retains Interest (no purge)
	if !m2.PeerWants("n1", 42, geoCell(1, 2)) {
		t.Fatal("Interest should survive soft replace")
	}

	// Stale reader apply gate: Interest clear framed as oldConn must not clobber state.
	emptyPay := EncodeInterest(InterestPayload{NodeID: "n1", Entries: nil})
	ok := m2.handleCtrlFrame("n1", MeshFrame{Type: MeshTypeInterest, Payload: emptyPay}, oldConn)
	if !ok {
		t.Fatal("stale handleCtrlFrame should return true (no death)")
	}
	if !m2.PeerWants("n1", 42, geoCell(1, 2)) {
		t.Fatal("stale reader must not clear Interest after soft replace")
	}
	// Stale directory delta must also be ignored
	deltaPay := EncodeTrxDelta(TrxDeltaPayload{
		OriginNodeID: "n1", Callsign: "STALE", IsATC: false,
	})
	_ = m2.handleCtrlFrame("n1", MeshFrame{Type: MeshTypeTrxDelta, Payload: deltaPay}, oldConn)
	// live generation apply still works
	live := m2.CurrentConnForTest("n1")
	if live == nil {
		t.Fatal("expected live conn after replace")
	}
	newPay := EncodeInterest(InterestPayload{NodeID: "n1", Entries: []InterestEntry{{FreqHz: 99, ILat: 3, ILon: 4}}})
	if !m2.handleCtrlFrame("n1", MeshFrame{Type: MeshTypeInterest, Payload: newPay}, live) {
		t.Fatal("live handleCtrlFrame failed")
	}
	if !m2.PeerWants("n1", 99, geoCell(3, 4)) {
		t.Fatal("live generation must still apply Interest")
	}

	// True death: close the live generation — OnPeerDead must fire, Interest cleared.
	m2.ForcePeerDeathForTest("n1")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if deadN1.Load() > 0 && !m2.PeerAuthedForTest("n1") && !m2.PeerWants("n1", 99, geoCell(3, 4)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("true death: dead=%d authed=%v wants=%v",
		deadN1.Load(), m2.PeerAuthedForTest("n1"), m2.PeerWants("n1", 99, geoCell(3, 4)))
}

func TestHybridMesh_EnqueueDuringDeathNoDeadlock(t *testing.T) {
	// Stress: EnqueueAudioRelay while ForcePeerDeath (lock-order regression).
	m1, m2, _ := startHybridPair(t, "psk")
	m1.ApplyInterestDirect("n2", []InterestEntry{{
		FreqHz: 122800000,
		ILat:   geo.CellIndex(51.5, geo.DefaultGridCellDeg),
		ILon:   geo.CellIndex(-0.1, geo.DefaultGridCellDeg),
	}})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			m1.EnqueueAudioRelay(AudioRelay{
				OriginNode: "n1", Callsign: "X", SequenceCounter: uint32(i),
				Audio:    []byte{1, 2, 3},
				TxRadios: []RelayTxRadio{{FreqHz: 122800000, LatDeg: 51.5, LonDeg: -0.1}},
			})
		}
	}()
	for i := 0; i < 30; i++ {
		m1.ForcePeerDeathForTest("n2")
		_ = m1.WaitPeerAuthed("n2", 500*time.Millisecond)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("enqueue+death deadlock or hang")
	}
	_ = m2
}
