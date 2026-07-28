package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// Abort matrix unit tests (issue 1 / 19 / R2-3) using MemoryMesh without full gnet.
func TestClaimAbortMatrix_ReserveOnly(t *testing.T) {
	hub := cluster.NewMemoryHub()
	peers := []string{"n1", "n2"}
	m1, _ := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{NodeID: "n1", PeerIDs: peers, ClaimTimeout: time.Second})
	m2, _ := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{NodeID: "n2", PeerIDs: peers, ClaimTimeout: time.Second})
	_ = m1.Start(context.Background())
	_ = m2.Start(context.Background())
	defer m1.Stop()
	defer m2.Stop()

	ctx := context.Background()
	fence, err := m1.ClaimReserve(ctx, "ABORT1", cluster.ClaimMeta{NodeID: "n1", CID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// Teardown after Reserve: ClaimRelease(fence) clears pending.
	m1.ClaimRelease("ABORT1", fence)
	if _, err := m2.ClaimReserve(ctx, "ABORT1", cluster.ClaimMeta{NodeID: "n2", CID: 2}); err != nil {
		t.Fatalf("after release should allow reserve: %v", err)
	}
}

func TestClaimAbortMatrix_RegisterBeforeCommit(t *testing.T) {
	hub := cluster.NewMemoryHub()
	m, _ := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{NodeID: "solo", PeerIDs: []string{"solo"}, ClaimTimeout: time.Second})
	_ = m.Start(context.Background())
	defer m.Stop()

	po := postoffice.New()
	hr := NewHybridRegistry(po, m)
	ctx := context.Background()
	fence, err := m.ClaimReserve(ctx, "REG1", cluster.ClaimMeta{NodeID: "solo", CID: 1})
	if err != nil {
		t.Fatal(err)
	}
	data := session.LoginData{Callsign: "REG1", CID: 1, ProtoRevision: 100, LoginTime: time.Now()}
	s := session.New(ctx, nil, nil, data)
	if err := hr.Register(s); err != nil {
		t.Fatal(err)
	}
	// R2-3: ClaimRelease clears pending (not only Abort).
	m.ClaimRelease("REG1", fence)
	hr.Local().Release(s)
	if _, err := m.ClaimReserve(ctx, "REG1", cluster.ClaimMeta{NodeID: "solo", CID: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestClaimAbortMatrix_CommitThenRelease(t *testing.T) {
	hub := cluster.NewMemoryHub()
	m, _ := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{NodeID: "solo", PeerIDs: []string{"solo"}, ClaimTimeout: time.Second})
	_ = m.Start(context.Background())
	defer m.Stop()
	po := postoffice.New()
	hr := NewHybridRegistry(po, m)
	ctx := context.Background()
	fence, _ := m.ClaimReserve(ctx, "OK1", cluster.ClaimMeta{NodeID: "solo"})
	s := session.New(ctx, nil, nil, session.LoginData{Callsign: "OK1", CID: 1, LoginTime: time.Now()})
	_ = hr.Register(s)
	if err := m.ClaimCommit(ctx, "OK1", fence); err != nil {
		t.Fatal(err)
	}
	hr.StoreFence("OK1", fence)
	hr.Release(s)
	if _, _, ok := m.Lookup("OK1"); ok {
		t.Fatal("should not be routable after release")
	}
}

func TestClaimEmptyFenceNoForce(t *testing.T) {
	ct := cluster.NewClaimTable(time.Second)
	f, err := ct.Reserve("X", cluster.ClaimMeta{NodeID: "a", Fence: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ct.Commit("X", f)
	if ct.Release("X", "") {
		t.Fatal("empty fence must not free active claim")
	}
	if !ct.Release("X", f) {
		t.Fatal("matching fence should release")
	}
}

func TestPeerDeathSyntheticDA(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1, n2 := h.node("n1"), h.node("n2")
	ctx := context.Background()
	fence, err := n2.mesh.ClaimReserve(ctx, "BOS_TWR", cluster.ClaimMeta{NodeID: "n2", CID: 50, IsATC: true})
	if err != nil {
		t.Fatal(err)
	}
	data := session.LoginData{
		Callsign: "BOS_TWR", CID: 50, IsAtc: true, NetworkRating: protocol.NetworkRatingController1,
		ProtoRevision: 100, LoginTime: time.Now(),
	}
	atc := session.New(ctx, nil, nil, data)
	if err := n2.reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	if err := n2.mesh.ClaimCommit(ctx, "BOS_TWR", fence); err != nil {
		t.Fatal(err)
	}

	// Capture outbound leave packets on n1 local session.
	var mu sync.Mutex
	var packets []string
	out, err := session.NewCoalesceOutbound(
		func(p []byte) error {
			mu.Lock()
			packets = append(packets, string(p))
			mu.Unlock()
			return nil
		},
		func() error { return nil },
		session.CoalesceOutboundConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	localData := session.LoginData{Callsign: "LOCAL1", CID: 99, ProtoRevision: 100, LoginTime: time.Now()}
	local := session.New(ctx, nil, nil, localData)
	local.SetOutbound(out)
	if err := n1.reg.Register(local); err != nil {
		t.Fatal(err)
	}
	// Directory join ATC on n1 view
	n1.mesh.NotifyLocalJoin(cluster.DirMeta{Callsign: "BOS_TWR", NodeID: "n2", IsATC: true, Routable: true})
	// Direct peer-death handler with known ATC meta (production path shape).
	n1.srv.handleMeshPeerDead("n2", []cluster.DirMeta{
		{Callsign: "BOS_TWR", NodeID: "n2", IsATC: true},
	})
	// Wait for async write
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := strings.Join(packets, "")
		mu.Unlock()
		if strings.Contains(got, "#DABOS_TWR") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	t.Fatalf("expected #DA for ATC, got %q", packets)
}

func TestPeerDeathSyntheticDP(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1 := h.node("n1")
	ctx := context.Background()
	var mu sync.Mutex
	var packets []string
	out, err := session.NewCoalesceOutbound(
		func(p []byte) error {
			mu.Lock()
			packets = append(packets, string(p))
			mu.Unlock()
			return nil
		},
		func() error { return nil },
		session.CoalesceOutboundConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	local := session.New(ctx, nil, nil, session.LoginData{Callsign: "LOCAL2", CID: 98, LoginTime: time.Now()})
	local.SetOutbound(out)
	if err := n1.reg.Register(local); err != nil {
		t.Fatal(err)
	}
	n1.srv.handleMeshPeerDead("n2", []cluster.DirMeta{
		{Callsign: "N123AB", NodeID: "n2", IsATC: false},
	})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := strings.Join(packets, "")
		mu.Unlock()
		if strings.Contains(got, "#DPN123AB") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	t.Fatalf("expected #DP for pilot, got %q", packets)
}

func TestSF_ViaPositionBatch(t *testing.T) {
	// R2-12: production path ForwardRanged → OnPositionBatch → handleMeshPositionBatch → SendProximityHint
	h := startMultiNode(t, "n1", "n2")
	n1, n2 := h.node("n1"), h.node("n2")
	pilot := loginLocal(t, n1, "SFPATH", 7, false)
	// Place pilot near NYC so receiver distance is small
	pilot.SetGeo(40.7, -74.0, 50*1852)

	// Receiver pilot on n2 within range of sender boxes
	p2 := loginLocal(t, n2, "RECVSF", 8, false)
	p2.SetGeo(40.71, -74.01, 50*1852)
	// Proto 101 for proximity
	p2.ProtoRevision = 101
	// Ensure interest covers area so ForwardRanged delivers
	n2.mesh.PublishInterest(cluster.InterestSummary{
		NodeID: "n2",
		Boxes:  []cluster.AABB{{MinLat: 40, MaxLat: 41, MinLon: -75, MaxLon: -73}},
	})

	// Wire production handlers on both sides
	n2.mesh.OnPositionBatch(func(from string, batch cluster.PositionBatch) {
		n2.srv.handleMeshPositionBatch(from, batch)
	})
	n1.mesh.OnProximityHint(func(from, cs string, d float64) {
		n1.reg.onProximityHint(from, cs, d)
	})

	// Production forward with SenderCallsign (as meshForwardPosition does)
	n1.mesh.ForwardRanged(
		[]byte("@SFPATH:...\r\n"),
		[]cluster.AABB{{MinLat: 40.6, MaxLat: 40.8, MinLon: -74.1, MaxLon: -73.9}},
		cluster.RangeClassPosition,
		"SFPATH",
	)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		min := n1.reg.RemoteClosestM("SFPATH", time.Now())
		if min < 20*1852 { // within 20 NM of RECVSF
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("remote closest not set via PositionBatch production path: %v", n1.reg.RemoteClosestM("SFPATH", time.Now()))
}

func TestHomeRPC_AmendOffPool(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1, n2 := h.node("n1"), h.node("n2")
	pilot := loginLocal(t, n1, "FPL1", 11, false)
	// Amend from n2 via meshHomeRPC (async pool)
	done := make(chan struct{})
	n2.srv.meshHomeRPC(nil, "FPL1", cluster.HomeOpMutateFlightPlan, []byte("VFR:TEST"),
		func(_ []byte) { close(done) },
		func(err error) { t.Errorf("unexpected err %v", err); close(done) },
	)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	if pilot.FlightPlan.Load() != "VFR:TEST" {
		t.Fatalf("fpl %q", pilot.FlightPlan.Load())
	}
}
