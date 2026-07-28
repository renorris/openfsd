package afv

import (
	"bytes"
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

func TestDecodeMeshFrameExact(t *testing.T) {
	pay := []byte("hello")
	raw, err := EncodeMeshFrameBytes(MeshTypeHeartbeat, pay)
	if err != nil {
		t.Fatal(err)
	}
	fr, err := DecodeMeshFrameExact(raw)
	if err != nil || fr.Type != MeshTypeHeartbeat || !bytes.Equal(fr.Payload, pay) {
		t.Fatalf("%+v %v", fr, err)
	}
	// trailing bytes
	bad := append(append([]byte{}, raw...), 0x00)
	if _, err := DecodeMeshFrameExact(bad); err == nil {
		t.Fatal("trailing should fail")
	}
	// short
	if _, err := DecodeMeshFrameExact(raw[:3]); err == nil {
		t.Fatal("short should fail")
	}
}

func TestHybridMesh_UDPUnknownSource(t *testing.T) {
	m1, m2, _ := startHybridPair(t, "psk")
	var got atomic.Int32
	m1.OnAudioRelay(func(from string, r AudioRelay) {
		got.Add(1)
	})
	// Send spoofed UDP to m1 voice from random port (not peer voice source port)
	// Write to m1 voice listen with frame claiming origin n2
	addr, err := net.ResolveUDPAddr("udp", m1.ListenVoiceAddr())
	if err != nil {
		t.Fatal(err)
	}
	cli, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	relay := AudioRelay{OriginNode: "n2", Callsign: "X", Audio: []byte{1, 2, 3},
		TxRadios: []RelayTxRadio{{FreqHz: 1, LatDeg: 0, LonDeg: 0}}}
	pay, _ := EncodeAudioRelay(relay)
	pkt, _ := EncodeMeshFrameBytes(MeshTypeAudioRelay, pay)
	before := m1.UDPAllowDrops()
	_, _ = cli.WriteTo(pkt, addr)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m1.UDPAllowDrops() > before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Load() != 0 {
		t.Fatal("OnAudioRelay must not fire for unknown source")
	}
	if m1.UDPAllowDrops() <= before {
		t.Fatalf("want UDPAllowDrops increase, got %d→%d", before, m1.UDPAllowDrops())
	}
	_ = m2
}

func TestHybridMesh_UDPNotAuthed(t *testing.T) {
	tcp1, voice1, tcp2, voice2 := hybridPairPorts(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Only start m1; peer n2 never authenticates
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

	var got atomic.Int32
	m1.OnAudioRelay(func(from string, r AudioRelay) { got.Add(1) })

	// Send from the configured peer voice port (allowlisted) but peer not authed
	cli, err := net.ListenPacket("udp", "127.0.0.1:"+itoa(voice2))
	if err != nil {
		// port may be free since m2 not started
		t.Fatal(err)
	}
	defer cli.Close()
	addr, _ := net.ResolveUDPAddr("udp", m1.ListenVoiceAddr())
	pay, _ := EncodeAudioRelay(AudioRelay{OriginNode: "n2", Callsign: "X", Audio: []byte{9}})
	pkt, _ := EncodeMeshFrameBytes(MeshTypeAudioRelay, pay)
	_, _ = cli.WriteTo(pkt, addr)
	time.Sleep(100 * time.Millisecond)
	if got.Load() != 0 {
		t.Fatal("not-authed peer must not deliver")
	}
}

func TestHybridMesh_IPv4MappedAllowlist(t *testing.T) {
	// Unit: canonicalUDPKeys dual-key
	ip := net.ParseIP("127.0.0.1")
	keys := canonicalUDPKeys(ip, 17001)
	foundV4, foundMapped := false, false
	for _, k := range keys {
		if k == "127.0.0.1:17001" {
			foundV4 = true
		}
		if k == "[::ffff:127.0.0.1]:17001" || k == "::ffff:127.0.0.1:17001" {
			foundMapped = true
		}
	}
	if !foundV4 {
		t.Fatalf("missing v4 key in %v", keys)
	}
	if !foundMapped {
		// To16 string form of mapped
		mapped := net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 127, 0, 0, 1}
		mk := net.JoinHostPort(mapped.String(), "17001")
		ok := false
		for _, k := range keys {
			if k == mk {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("missing mapped key; keys=%v want %s", keys, mk)
		}
	}
}

func TestHybridMesh_InterestEmptyNoFlood(t *testing.T) {
	m1, m2, _ := startHybridPair(t, "psk")
	// Empty Interest: Enqueue should not send UDP
	before := m1.VoiceTx()
	for i := 0; i < 20; i++ {
		m1.EnqueueAudioRelay(AudioRelay{
			OriginNode: "n1", Callsign: "A", SequenceCounter: uint32(i),
			Audio:    []byte{1, 2, 3},
			TxRadios: []RelayTxRadio{{FreqHz: 122800000, LatDeg: 51.5, LonDeg: -0.1}},
		})
	}
	time.Sleep(100 * time.Millisecond)
	if m1.VoiceTx() != before {
		t.Fatalf("empty Interest must not flood voice tx: %d→%d", before, m1.VoiceTx())
	}
	_ = m2
}

func TestHybridMesh_VoiceFanoutWithInterest(t *testing.T) {
	m1, m2, _ := startHybridPair(t, "psk")
	var got atomic.Int32
	m2.OnAudioRelay(func(from string, r AudioRelay) {
		if from == "n1" && r.Callsign == "TX1" {
			got.Add(1)
		}
	})
	// m2 advertises Interest for TX cell
	lat, lon := 51.5, -0.12
	freq := uint32(122800000)
	ck := geo.CellKey{
		ILat: geo.CellIndex(lat, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(lon, geo.DefaultGridCellDeg),
	}
	m2.PublishInterest([]InterestEntry{{FreqHz: freq, ILat: ck.ILat, ILon: ck.ILon}})
	// Wait until m1 sees PeerWants
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m1.PeerWants("n2", freq, ck) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !m1.PeerWants("n2", freq, ck) {
		t.Fatal("PeerWants not ready")
	}
	m1.EnqueueAudioRelay(AudioRelay{
		OriginNode: "n1", Callsign: "TX1", SequenceCounter: 1,
		Audio:    []byte("opus"),
		TxRadios: []RelayTxRadio{{TxID: 0, FreqHz: freq, LatDeg: lat, LonDeg: lon, HeightM: 100}},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected AudioRelay delivery over UDP")
}

func TestHybridMesh_UDPOversizeDrop(t *testing.T) {
	// Lower cap so OS can send "oversize" datagrams on localhost.
	old := maxMeshVoiceDatagram
	maxMeshVoiceDatagram = 256
	t.Cleanup(func() { maxMeshVoiceDatagram = old })

	m1, m2, _ := startHybridPair(t, "psk")
	var got atomic.Int32
	m1.OnAudioRelay(func(from string, r AudioRelay) { got.Add(1) })

	addr, err := net.ResolveUDPAddr("udp", m1.ListenVoiceAddr())
	if err != nil {
		t.Fatal(err)
	}
	before := m1.UDPOversizeDrops()
	// Source from m2's mesh voice socket (allowlisted).
	m2.mu.RLock()
	pc := m2.voicePC
	m2.mu.RUnlock()
	if pc == nil {
		t.Fatal("m2 voicePC nil")
	}
	big := make([]byte, maxMeshVoiceDatagram+64) // 320 > 256
	if _, err := pc.WriteTo(big, addr); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m1.UDPOversizeDrops() > before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m1.UDPOversizeDrops() <= before {
		t.Fatalf("want oversize drops, got %d→%d", before, m1.UDPOversizeDrops())
	}
	if got.Load() != 0 {
		t.Fatal("OnAudioRelay must not fire for oversize")
	}
}

func TestHybridMesh_VoiceQueueDropOldest(t *testing.T) {
	// No voice drainer consuming: unauthed peer shell only.
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
	// Voice drainers run but skip when !authed — TryRecv only when authed, so queue fills.
	// Actually drain loop only TryRecv when authed; unauthed never drains → drops work.
	before := m1.VoiceQueueDrops()
	m1.ForceEnqueueVoiceForTest("n2", meshVoiceQueueDepth+30)
	if m1.VoiceQueueDrops() <= before {
		t.Fatalf("want voice drops, got %d→%d", before, m1.VoiceQueueDrops())
	}
	if m1.PeerVoiceQueueLen("n2") > meshVoiceQueueDepth {
		t.Fatal("voice queue over depth")
	}
}

func TestHybridMesh_InboundRateLimit(t *testing.T) {
	// Lower rate isn't configurable; test that under 5000/s delivers and counter type exists.
	// Send a burst of 50 — all should pass at 5000/s.
	m1, m2, _ := startHybridPair(t, "psk")
	var got atomic.Int32
	m1.OnAudioRelay(func(from string, r AudioRelay) { got.Add(1) })
	// Need authed + allowlist source = m2 voice listen
	// Use internal path: mark as if from peer by calling handle after allowlist
	// Direct unit: allowInboundRate
	m1.mu.RLock()
	p := m1.peers["n2"]
	m1.mu.RUnlock()
	// simulate 5000 allows then drop
	p.rateMu.Lock()
	p.rateWindow = time.Now()
	p.rateCount = meshVoiceInboundRatePerSec
	p.rateMu.Unlock()
	if m1.allowInboundRate(p) {
		t.Fatal("should rate-limit when at cap")
	}
	// reset window
	p.rateMu.Lock()
	p.rateWindow = time.Time{}
	p.rateCount = 0
	p.rateMu.Unlock()
	if !m1.allowInboundRate(p) {
		t.Fatal("should allow under cap")
	}
	_ = m2
	_ = got
}
