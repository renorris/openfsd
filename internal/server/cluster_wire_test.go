package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/session"
)

func TestSessionOverlapsSenderBoxes(t *testing.T) {
	s := session.New(t.Context(), nil, nil, session.LoginData{Callsign: "X"})
	s.SetGeo(40.5, -73.5, 50*1852)
	boxes := []cluster.AABB{{MinLat: 40, MaxLat: 41, MinLon: -74, MaxLon: -73}}
	if !sessionOverlapsSenderBoxes(s, boxes) {
		t.Fatal("expected overlap")
	}
	if !sessionOverlapsSenderBoxes(s, nil) {
		t.Fatal("empty boxes true")
	}
	far := []cluster.AABB{{MinLat: 0, MaxLat: 1, MinLon: 0, MaxLon: 1}}
	if sessionOverlapsSenderBoxes(s, far) {
		t.Fatal("no overlap")
	}
}

func TestSessionSenderBoxes(t *testing.T) {
	s := session.New(t.Context(), nil, nil, session.LoginData{Callsign: "A", IsAtc: true})
	s.SetGeo(10, 20, 1852)
	s.SetSecondaryVisCenter(0, 11, 21)
	boxes := sessionSenderBoxes(s)
	if len(boxes) < 2 {
		t.Fatalf("%d", len(boxes))
	}
}

func TestHybridInterestBuild(t *testing.T) {
	// pure interest build via HybridRegistry
	h := startMultiNode(t, "a", "b")
	n := h.node("a")
	_ = loginLocal(t, n, "P", 1, false)
	sum := n.reg.buildInterestSummary()
	if sum.NodeID == "" {
		t.Fatal()
	}
	n.reg.markInterestDirty()
	n.reg.StopInterest()
}

func TestHandleMeshHomeRPCAllOps(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1 := h.node("n1")
	p := loginLocal(t, n1, "HMRPC", 3, false)
	// mutate
	_, err := n1.srv.handleMeshHomeRPC(cluster.HomeOpMutateFlightPlan, "HMRPC", []byte("IFR"))
	if err != nil {
		t.Fatal(err)
	}
	if p.FlightPlan.Load() != "IFR" {
		t.Fatal(p.FlightPlan.Load())
	}
	_, err = n1.srv.handleMeshHomeRPC(cluster.HomeOpAssignBeacon, "HMRPC", []byte("1200"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := n1.srv.handleMeshHomeRPC(cluster.HomeOpQuerySessionMeta, "HMRPC", nil)
	if err != nil || len(resp) == 0 {
		t.Fatal(err)
	}
	// ForceDisconnect
	_, err = n1.srv.handleMeshHomeRPC(cluster.HomeOpForceDisconnect, "HMRPC", nil)
	if err != nil {
		t.Fatal(err)
	}
	// missing
	if _, err := n1.srv.handleMeshHomeRPC(cluster.HomeOpQuerySessionMeta, "NOPE", nil); err == nil {
		t.Fatal("want not found")
	}
}

func TestHandleMeshDirectWireClasses(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1 := h.node("n1")
	_ = loginLocal(t, n1, "ATC1", 4, true)
	_ = loginLocal(t, n1, "PIL1", 5, false)
	// ATC broadcast class
	payload := append([]byte{byte(cluster.BroadcastATC)}, []byte("#TMx:*A:hi\r\n")...)
	n1.srv.handleMeshDirectWire("n2", payload, cluster.WireBroadcast)
	// supervisor
	payload = append([]byte{byte(cluster.BroadcastSupervisor)}, []byte("#TMx:*S:w\r\n")...)
	n1.srv.handleMeshDirectWire("n2", payload, cluster.WireBroadcast)
	// join leave
	n1.srv.handleMeshDirectWire("n2", []byte("#DPX:SERVER:0\r\n"), cluster.WireJoinLeave)
	// direct to local
	n1.srv.handleMeshDirectWire("n2", []byte("#TMPIL1:PIL1:hi\r\n"), cluster.WireDirect)
	// peer death ATC
	n1.srv.handleMeshPeerDead("dead", []cluster.DirMeta{
		{Callsign: "X_TWR", IsATC: true},
		{Callsign: "N1", IsATC: false},
	})
}

func TestSECPOSInInterestSummary(t *testing.T) {
	h := startMultiNode(t, "a", "b")
	n := h.node("a")
	ctx := context.Background()
	atc := session.New(ctx, nil, nil, session.LoginData{
		Callsign: "LAX_CTR", CID: 1, IsAtc: true, LoginTime: time.Now(),
	})
	// Primary near 34°N; SECPOS secondary near 36°N so boxes cannot collapse to primary-only.
	atc.SetGeo(34.0, -118.0, 100*1852)
	if !atc.SetSecondaryVisCenter(0, 36.0, -118.0) {
		t.Fatal("secpos")
	}
	if err := n.reg.Register(atc); err != nil {
		t.Fatal(err)
	}
	n.reg.markInterestDirty()
	sum := n.reg.buildInterestSummary()
	if len(sum.Boxes) < 1 {
		t.Fatal("expected interest boxes")
	}
	// Hard-fail if SECPOS secondary is missing: primary-only must not pass.
	// After merge/union the secondary center (lat 36) must still be covered by some box.
	foundSec := false
	for _, b := range sum.Boxes {
		if b.MinLat <= 36 && b.MaxLat >= 36 && b.MinLon <= -118 && b.MaxLon >= -118 {
			foundSec = true
			break
		}
	}
	if !foundSec {
		t.Fatalf("SECPOS secondary center (36,-118) missing from interest summary boxes=%+v", sum.Boxes)
	}
	// Primary still covered
	foundPri := false
	for _, b := range sum.Boxes {
		if b.MinLat <= 34 && b.MaxLat >= 34 && b.MinLon <= -118 && b.MaxLon >= -118 {
			foundPri = true
			break
		}
	}
	if !foundPri {
		t.Fatalf("primary center (34,-118) missing from interest summary boxes=%+v", sum.Boxes)
	}
	// Clear SECPOS dirties interest
	atc.ClearSecondaryVisCenters()
	n.srv.markInterestDirtyIfHybrid()
}

// TestReleaseClaimOnDisconnect exercises the shared OnClose claim teardown helper
// (releaseClaimOnDisconnect), covering the KD-4 / R2-3 matrix branches.
func TestReleaseClaimOnDisconnect(t *testing.T) {
	ctx := context.Background()
	newMesh := func(t *testing.T) *cluster.MemoryMesh {
		t.Helper()
		hub := cluster.NewMemoryHub()
		m, err := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{NodeID: "solo", PeerIDs: []string{"solo"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { m.Stop() })
		return m
	}
	// After helper, callsign must be free for a new Reserve.
	assertFree := func(t *testing.T, m *cluster.MemoryMesh, cs string) {
		t.Helper()
		if _, err := m.ClaimReserve(ctx, cs, cluster.ClaimMeta{NodeID: "solo"}); err != nil {
			t.Fatalf("claim stuck after teardown (%s): %v", cs, err)
		}
	}

	t.Run("reserve_only", func(t *testing.T) {
		m := newMesh(t)
		fence, err := m.ClaimReserve(ctx, "DISC1", cluster.ClaimMeta{NodeID: "solo"})
		if err != nil {
			t.Fatal(err)
		}
		// OnClose path: not committed, not registered.
		releaseClaimOnDisconnect(m, "DISC1", fence, false, false)
		assertFree(t, m, "DISC1")
	})

	t.Run("registered_not_committed", func(t *testing.T) {
		m := newMesh(t)
		fence, err := m.ClaimReserve(ctx, "DISC2", cluster.ClaimMeta{NodeID: "solo"})
		if err != nil {
			t.Fatal(err)
		}
		// Register before Commit (mid B): OnClose must still ClaimRelease(fence).
		releaseClaimOnDisconnect(m, "DISC2", fence, false, true)
		assertFree(t, m, "DISC2")
	})

	t.Run("committed_and_registered_skips_mesh", func(t *testing.T) {
		m := newMesh(t)
		fence, err := m.ClaimReserve(ctx, "DISC3", cluster.ClaimMeta{NodeID: "solo"})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.ClaimCommit(ctx, "DISC3", fence); err != nil {
			t.Fatal(err)
		}
		// OnClose: committed+registered → HybridRegistry.Release owns ClaimRelease.
		// Helper must not free the claim itself (would double-release / race with registry).
		releaseClaimOnDisconnect(m, "DISC3", fence, true, true)
		if _, _, ok := m.Lookup("DISC3"); !ok {
			t.Fatal("committed+registered path must leave claim for HybridRegistry.Release")
		}
		// Registry path frees it.
		m.ClaimRelease("DISC3", fence)
		assertFree(t, m, "DISC3")
	})

	t.Run("noop_empty_fence", func(t *testing.T) {
		m := newMesh(t)
		fence, err := m.ClaimReserve(ctx, "DISC4", cluster.ClaimMeta{NodeID: "solo"})
		if err != nil {
			t.Fatal(err)
		}
		releaseClaimOnDisconnect(m, "DISC4", "", false, false)
		// Still held — empty fence must not force-free.
		if _, err := m.ClaimReserve(ctx, "DISC4", cluster.ClaimMeta{NodeID: "solo"}); err == nil {
			t.Fatal("empty fence no-op must leave claim held")
		}
		releaseClaimOnDisconnect(m, "DISC4", fence, false, false)
		assertFree(t, m, "DISC4")
	})

	t.Run("noop_nil_mesh", func(t *testing.T) {
		// Must not panic.
		releaseClaimOnDisconnect(nil, "X", "f", false, false)
	})
}

// TestMeshClaimReleaseAfterReserveFreesCallsign is a mesh-only check that
// ClaimRelease after Reserve frees the callsign (not OnClose / gnet).
func TestMeshClaimReleaseAfterReserveFreesCallsign(t *testing.T) {
	hub := cluster.NewMemoryHub()
	m, _ := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{NodeID: "solo", PeerIDs: []string{"solo"}})
	_ = m.Start(context.Background())
	defer m.Stop()
	fence, err := m.ClaimReserve(context.Background(), "MESH1", cluster.ClaimMeta{NodeID: "solo", Fence: "f-mesh"})
	if err != nil {
		t.Fatal(err)
	}
	m.ClaimRelease("MESH1", fence)
	m.ClaimAbort("MESH1", fence)
	if _, err := m.ClaimReserve(context.Background(), "MESH1", cluster.ClaimMeta{NodeID: "solo"}); err != nil {
		t.Fatalf("stuck claim: %v", err)
	}
}

func TestMeshHomeRPCPoolFullFailClosed(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1 := h.node("n1")
	// Fill auth pool
	for i := 0; i < 300; i++ {
		select {
		case n1.srv.authPool <- func() { time.Sleep(50 * time.Millisecond) }:
		default:
		}
	}
	var failed atomic.Bool
	n1.srv.meshHomeRPC(nil, "NOPE", cluster.HomeOpQuerySessionMeta, nil,
		func([]byte) {},
		func(err error) { failed.Store(true) },
	)
	// Either enqueued or fail-closed; if pool full must fail closed
	if len(n1.srv.authPool) >= 256 {
		// may still have taken a slot; drain a bit
	}
	// Force full
	for len(n1.srv.authPool) < 256 {
		select {
		case n1.srv.authPool <- func() { time.Sleep(200 * time.Millisecond) }:
		default:
			goto full
		}
	}
full:
	failed.Store(false)
	n1.srv.meshHomeRPC(nil, "NOPE", cluster.HomeOpQuerySessionMeta, nil,
		func([]byte) { t.Error("should not succeed on full pool") },
		func(err error) { failed.Store(true) },
	)
	if !failed.Load() {
		t.Fatal("expected fail closed when pool full")
	}
}
