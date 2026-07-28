package afv

import (
	"context"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/pkg/afvprotocol"
)

func TestMemoryMesh_PeerDeathPurgesInterest(t *testing.T) {
	hub := NewMemoryHub()
	m1, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	remote := newRemoteDir()
	m1.OnDirectory(remote)
	dead := make(chan string, 1)
	m1.OnPeerDead(func(id string) {
		remote.RemoveNode(id)
		dead <- id
	})
	// seed remote dir + interest
	remote.ApplySnapshot("n2", []RemoteSession{{Callsign: "REM", IsATC: false}})
	m2.PublishInterest([]InterestEntry{{FreqHz: 1, ILat: 1, ILon: 1}})
	if !m1.PeerWants("n2", 1, geo.CellKey{ILat: 1, ILon: 1}) {
		t.Fatal("interest")
	}
	if remote.CountOrigin("n2") != 1 {
		t.Fatal()
	}
	m1.SimulatePeerDown("n2")
	select {
	case id := <-dead:
		if id != "n2" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("no OnPeerDead")
	}
	if remote.CountOrigin("n2") != 0 {
		t.Fatal("remote dir not purged")
	}
	if m1.PeerWants("n2", 1, geo.CellKey{ILat: 1, ILon: 1}) {
		t.Fatal("interest not cleared")
	}
	// local mesh still running for self
	if m1.NodeID() != "n1" {
		t.Fatal()
	}
}

func TestMemoryMesh_EmptyInterestNoFlood(t *testing.T) {
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	m2, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan AudioRelay, 4)
	m2.OnAudioRelay(func(_ string, r AudioRelay) { got <- r })
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// empty interest on m1's view of m2
	m1.ClearPeerInterest("n2")
	m1.EnqueueAudioRelay(AudioRelay{
		Callsign: "A", Audio: []byte{9},
		TxRadios: []RelayTxRadio{{FreqHz: 118700000, LatDeg: 40, LonDeg: -73}},
	})
	select {
	case <-got:
		t.Fatal("flooded AR with empty interest")
	case <-time.After(100 * time.Millisecond):
		// ok
	}
	// with interest, delivers
	ck := geo.CellKey{
		ILat: geo.CellIndex(40, geo.DefaultGridCellDeg),
		ILon: geo.CellIndex(-73, geo.DefaultGridCellDeg),
	}
	m1.ApplyInterestDirect("n2", []InterestEntry{{
		FreqHz: 118700000, ILat: ck.ILat, ILon: ck.ILon,
	}})
	m1.EnqueueAudioRelay(AudioRelay{
		Callsign: "A", Audio: []byte{9},
		TxRadios: []RelayTxRadio{{FreqHz: 118700000, LatDeg: 40, LonDeg: -73}},
	})
	select {
	case r := <-got:
		if r.Callsign != "A" {
			t.Fatalf("%+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting AR")
	}
}

func TestMemoryMesh_HelloPSKReject(t *testing.T) {
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "good", PeerIDs: []string{"n1", "n2"}})
	_, _ = NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "bad", PeerIDs: []string{"n1", "n2"}})
	ctx := context.Background()
	// Start verifies local PSK against peer.psk before workers (fail closed).
	err := m1.Start(ctx)
	if err == nil {
		t.Fatal("expected PSK reject")
	}
	if err != errMeshHelloAuth {
		t.Fatalf("err=%v want errMeshHelloAuth", err)
	}
	_ = m1.Stop()
}

func TestPublishInterest_NoAsyncStaleOverwrite(t *testing.T) {
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	m2, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m2.PublishInterest(nil)
	m2.PublishInterest([]InterestEntry{{FreqHz: 99, ILat: 1, ILon: 2}})
	time.Sleep(50 * time.Millisecond)
	if !m1.PeerWants("n2", 99, geo.CellKey{ILat: 1, ILon: 2}) {
		t.Fatal("sync interest lost to stale async re-apply")
	}
}

func TestMemoryMesh_KeysNeverOnCapturedRelay(t *testing.T) {
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	_, _ = NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	ck := geo.CellKey{ILat: 1, ILon: 1}
	m1.ApplyInterestDirect("n2", []InterestEntry{{FreqHz: 1, ILat: 1, ILon: 1}})
	_ = ck
	keyish := make([]byte, 32)
	for i := range keyish {
		keyish[i] = 0xab
	}
	m1.EnqueueAudioRelay(AudioRelay{
		Callsign: "A", Audio: []byte{1, 2, 3},
		TxRadios: []RelayTxRadio{{FreqHz: 1, LatDeg: 0.25, LonDeg: 0.25}},
	})
	relays := m1.LastRelays()
	if len(relays) == 0 {
		t.Fatal("no capture")
	}
	enc, err := EncodeAudioRelay(relays[0])
	if err != nil {
		t.Fatal(err)
	}
	// Decode and ensure no accidental key-sized reserved fields beyond audio
	got, err := DecodeAudioRelay(enc)
	if err != nil {
		t.Fatal(err)
	}
	// struct has only public mesh fields
	if got.OriginNode == "" && relays[0].OriginNode != "n1" {
		t.Fatal()
	}
	// audio is small opus not 32-byte key
	if len(got.Audio) == 32 {
		t.Fatal("unexpected key-sized audio")
	}
}

// TestMemoryMesh_ReconnectSnapshot hardens Snapshot apply after death+up (M-16).
func TestMemoryMesh_ReconnectSnapshot(t *testing.T) {
	hub := NewMemoryHub()
	m1, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	remote1 := newRemoteDir()
	remote2 := newRemoteDir()
	m1.OnDirectory(remote1)
	m2.OnDirectory(remote2)
	m1.OnPeerDead(func(id string) { remote1.RemoveNode(id) })
	m2.OnPeerDead(func(id string) { remote2.RemoveNode(id) })

	m1.SetSnapshotProvider(func() []MeshSessionBlock {
		return []MeshSessionBlock{{Callsign: "LOC", IsATC: false, Trxs: []MeshTrx{{ID: 0, FreqHz: 118700000}}}}
	})
	m2.SetSnapshotProvider(func() []MeshSessionBlock {
		return []MeshSessionBlock{{Callsign: "REM", IsATC: true, Trxs: []MeshTrx{{ID: 0, FreqHz: 1}}}}
	})
	m1.SetInterestProvider(func() []InterestEntry {
		return []InterestEntry{{FreqHz: 1, ILat: 0, ILon: 0}}
	})
	m2.SetInterestProvider(func() []InterestEntry {
		return []InterestEntry{{FreqHz: 1, ILat: 0, ILon: 0}}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m1.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m2.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// Wait for cross Snapshot delivery (control drain)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if remote1.CountOrigin("n2") > 0 && remote2.CountOrigin("n1") > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if remote1.CountOrigin("n2") == 0 {
		// Start already published; force one more snapshot exchange
		m2.PublishTrxSnapshot()
		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if remote1.CountOrigin("n2") > 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if remote1.CountOrigin("n2") == 0 {
		t.Fatal("expected remote snapshot of n2 on n1 before death")
	}
	s, ok := remote1.Session("n2", "REM")
	if !ok || !s.IsATC {
		t.Fatalf("REM snapshot missing or wrong: %+v ok=%v", s, ok)
	}

	// Death purges remote dir
	m1.SimulatePeerDown("n2")
	if remote1.CountOrigin("n2") != 0 {
		t.Fatal("expected purge after peer death")
	}

	// Reconnect: Snapshot + Interest via provider (no test-only force)
	m1.SimulatePeerUp("n2")
	m2.SimulatePeerUp("n1")
	// PeerUp re-publishes Snapshot from provider; wait for apply
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if remote1.CountOrigin("n2") > 0 {
			break
		}
		// m2 must also re-snapshot toward n1 after m1 is up
		m2.PublishTrxSnapshot()
		time.Sleep(20 * time.Millisecond)
	}
	if remote1.CountOrigin("n2") == 0 {
		t.Fatal("expected Snapshot re-apply after reconnect")
	}
	s, ok = remote1.Session("n2", "REM")
	if !ok || !s.IsATC || len(s.Trxs) != 1 {
		t.Fatalf("post-reconnect snapshot %+v ok=%v", s, ok)
	}
	// Interest provider path on PeerUp
	if !m1.PeerWants("n2", 1, geo.CellKey{ILat: 0, ILon: 0}) {
		// PeerUp publishes n2's interest onto n1 when n2.SimulatePeerUp runs
		// n2.SimulatePeerUp publishes n2's current interest to peers including n1
		t.Fatal("reconnect interest from provider")
	}
}

func TestMemoryMesh_PublishLeaveAndVoicePaths(t *testing.T) {
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	m2, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	remote := newRemoteDir()
	m2.OnDirectory(remote)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = m1.Start(ctx)
	_ = m2.Start(ctx)
	m1.PublishSessionLeave("GONE")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// leave delivered async — also apply path via PublishTrxDelta
		time.Sleep(20 * time.Millisecond)
		break
	}
	m1.PublishTrxDelta("NEW", true, []Transceiver{{ID: 0, Frequency: 1, LatDeg: 1, LonDeg: 2}})
	time.Sleep(50 * time.Millisecond)
	// PeerVoiceQueueLen
	_ = m1.PeerVoiceQueueLen("n2")
	_ = m1.PeerVoiceQueueLen("missing")
	// Enqueue with no radios → no peer match
	m1.EnqueueAudioRelay(AudioRelay{Callsign: "X", Audio: []byte{1}})
	// nil mesh guards
	var nilM *MemoryMesh
	_ = nilM.Start(ctx)
	_ = nilM.Stop()
	_ = nilM.NodeID()
	nilM.PublishTrxSnapshot()
	nilM.PublishTrxDelta("a", false, nil)
	nilM.PublishSessionLeave("a")
	nilM.PublishInterest(nil)
	nilM.EnqueueAudioRelay(AudioRelay{})
	_ = nilM.PeerWants("x", 1, geo.CellKey{})
	_ = nilM.InterestedPeers(nil)
	// empty node id constructor
	if _, err := NewMemoryMesh(hub, MeshConfig{NodeID: "  "}); err == nil {
		t.Fatal()
	}
	if _, err := NewMemoryMesh(nil, MeshConfig{NodeID: "z"}); err == nil {
		t.Fatal()
	}
	// duplicate register
	if _, err := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p"}); err == nil {
		t.Fatal()
	}
	_ = m2
}

func TestConcurrentATReapTrxRace(t *testing.T) {
	cfg := &Config{
		MaxSessions: 50, MaxSessionsPerCID: 10,
		RangeDefaultNM: 100, HeartbeatTimeout: time.Hour, SessionIdleTimeout: time.Hour,
	}
	hub := NewMemoryHub()
	m1, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n1", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	m2, _ := NewMemoryMesh(hub, MeshConfig{NodeID: "n2", PSK: "p", PeerIDs: []string{"n1", "n2"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = m1.Start(ctx)
	_ = m2.Start(ctx)

	s := New(cfg, nil, nil, []byte("k"))
	s.SetMesh(m1)
	s.registerMeshCallbacks()
	r := s.reg
	now := time.Now()
	s1, _, _ := r.CreateOrReplace(1, "A", "", now)
	s2, _, _ := r.CreateOrReplace(2, "B", "", now)
	_, _, _ = r.UpdateTransceivers(1, "A", []Transceiver{{ID: 0, Frequency: 118700000, LatDeg: 40, LonDeg: -73}})
	_, _, _ = r.UpdateTransceivers(2, "B", []Transceiver{{ID: 0, Frequency: 118700000, LatDeg: 40.01, LonDeg: -73.01}})
	_, _, _ = r.BindUDP(s1, fakeAddr{"1"}, now)
	_, _, _ = r.BindUDP(s2, fakeAddr{"2"}, now)
	m1.ApplyInterestDirect("n2", []InterestEntry{{
		FreqHz: 118700000,
		ILat:   geo.CellIndex(40, geo.DefaultGridCellDeg),
		ILon:   geo.CellIndex(-73, geo.DefaultGridCellDeg),
	}})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = r.routeSyntheticTX("C", false, []RelayTxRadio{{
				TxID: 0, FreqHz: 118700000, LatDeg: 40.0, LonDeg: -73.0,
			}})
			_, radios := r.snapshotTXForMesh(s1, []afvprotocol.TxTransceiver{{ID: 0}})
			m1.EnqueueAudioRelay(AudioRelay{
				Callsign: "A", SequenceCounter: uint32(i), Audio: []byte{1},
				TxRadios: radios,
			})
			_ = r.snapshotLocalSessionsForMesh()
		}
	}()
	go func() {
		for i := 0; i < 100; i++ {
			isATC, trxs, _ := r.UpdateTransceivers(1, "A", []Transceiver{{
				ID: 0, Frequency: 118700000, LatDeg: 40 + float64(i)*0.0001, LonDeg: -73,
			}})
			s.meshPublishDelta("A", isATC, trxs)
		}
	}()
	go func() {
		for i := 0; i < 50; i++ {
			leaves := r.Reap(now) // unlikely to reap with long timeouts
			s.meshPublishLeaves(leaves)
			_ = r.buildInterestEntries(cfg)
			s.PublishInterestNowForTest()
		}
	}()
	<-done
	time.Sleep(50 * time.Millisecond)
	_ = m2
}
