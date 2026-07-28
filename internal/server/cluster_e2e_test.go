package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/renorris/openfsd/internal/cluster"
	"github.com/renorris/openfsd/internal/postoffice"
	"github.com/renorris/openfsd/internal/session"
	"github.com/renorris/openfsd/pkg/protocol"
)

// multiNodeHarness wires 2–3 HybridRegistries over MemoryMesh without full gnet.
type multiNodeHarness struct {
	hub   *cluster.MemoryHub
	nodes []*nodeH
}

type nodeH struct {
	id   string
	mesh *cluster.MemoryMesh
	reg  *HybridRegistry
	po   *postoffice.PostOffice
	srv  *Server
}

func startMultiNode(t *testing.T, ids ...string) *multiNodeHarness {
	t.Helper()
	if len(ids) < 2 {
		t.Fatal("need ≥2 nodes")
	}
	hub := cluster.NewMemoryHub()
	h := &multiNodeHarness{hub: hub}
	for _, id := range ids {
		m, err := cluster.NewMemoryMesh(hub, cluster.MemoryMeshConfig{
			NodeID:         id,
			PeerIDs:        ids,
			ClaimTimeout:   time.Second,
			PeerDeathGrace: 20 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		po := postoffice.New()
		hr := NewHybridRegistry(po, m)
		// minimal server for HomeRPC wiring
		srv, err := New(Deps{
			Config:   &Config{FsdListenAddrs: []string{":0"}, ServiceHTTPListenAddr: "127.0.0.1:0"},
			Users:    stubUserStore{},
			ConfigKV: stubConfigStore{},
			Registry: hr,
			Metar:    stubMetar{},
			Mesh:     m,
		})
		if err != nil {
			t.Fatal(err)
		}
		h.nodes = append(h.nodes, &nodeH{id: id, mesh: m, reg: hr, po: po, srv: srv})
	}
	t.Cleanup(func() {
		for _, n := range h.nodes {
			_ = n.mesh.Stop()
		}
	})
	return h
}

func (h *multiNodeHarness) node(id string) *nodeH {
	for _, n := range h.nodes {
		if n.id == id {
			return n
		}
	}
	return nil
}

func loginLocal(t *testing.T, n *nodeH, callsign string, cid int, isATC bool) *session.Session {
	t.Helper()
	ctx := context.Background()
	fence, err := n.mesh.ClaimReserve(ctx, callsign, cluster.ClaimMeta{NodeID: n.id, CID: cid, IsATC: isATC})
	if err != nil {
		t.Fatalf("reserve %s: %v", callsign, err)
	}
	data := session.LoginData{
		Callsign: callsign, CID: cid, RealName: "T", NetworkRating: protocol.NetworkRatingObserver,
		IsAtc: isATC, ProtoRevision: 101, LoginTime: time.Now(),
	}
	s := session.New(ctx, nil, nil, data)
	if err := n.reg.Register(s); err != nil {
		t.Fatal(err)
	}
	if err := n.mesh.ClaimCommit(ctx, callsign, fence); err != nil {
		t.Fatal(err)
	}
	n.reg.StoreFence(callsign, fence)
	return s
}

func TestE2E_ClusterConcurrentClaim(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	var wins int32
	var wg sync.WaitGroup
	for _, id := range []string{"n1", "n2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			n := h.node(id)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			fence, err := n.mesh.ClaimReserve(ctx, "RACECS", cluster.ClaimMeta{NodeID: id, CID: 1})
			if err != nil {
				return
			}
			data := session.LoginData{Callsign: "RACECS", CID: 1, ProtoRevision: 100, LoginTime: time.Now()}
			s := session.New(ctx, nil, nil, data)
			if err := n.reg.Register(s); err != nil {
				n.mesh.ClaimAbort("RACECS", fence)
				return
			}
			if err := n.mesh.ClaimCommit(ctx, "RACECS", fence); err != nil {
				n.reg.Release(s)
				n.mesh.ClaimAbort("RACECS", fence)
				return
			}
			atomic.AddInt32(&wins, 1)
		}(id)
	}
	wg.Wait()
	if atomic.LoadInt32(&wins) != 1 {
		t.Fatalf("wins=%d", wins)
	}
}

func TestE2E_ClusterHomeRPCAmendBeaconKill(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1, n2 := h.node("n1"), h.node("n2")
	pilot := loginLocal(t, n1, "N1PILOT", 10, false)
	_ = pilot

	// Amend FPL from n2
	ctx := context.Background()
	_, err := n2.reg.MeshHomeRPC(ctx, "N1PILOT", cluster.HomeOpMutateFlightPlan, []byte("IFR:TEST"))
	if err != nil {
		t.Fatal(err)
	}
	if pilot.FlightPlan.Load() != "IFR:TEST" {
		t.Fatalf("fpl %q", pilot.FlightPlan.Load())
	}
	_, err = n2.reg.MeshHomeRPC(ctx, "N1PILOT", cluster.HomeOpAssignBeacon, []byte("1234"))
	if err != nil {
		t.Fatal(err)
	}
	if pilot.AssignedBeaconCode.Load() != "1234" {
		t.Fatalf("beacon %q", pilot.AssignedBeaconCode.Load())
	}
	_, err = n2.reg.MeshHomeRPC(ctx, "N1PILOT", cluster.HomeOpForceDisconnect, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestE2E_ClusterPeerDeathSyntheticDP(t *testing.T) {
	h := startMultiNode(t, "n1", "n2")
	n1, n2 := h.node("n1"), h.node("n2")
	_ = loginLocal(t, n2, "ONN2", 20, false)

	// Local recipient on n1 to receive synthetic leave
	data := session.LoginData{Callsign: "LOCAL", CID: 99, ProtoRevision: 100, LoginTime: time.Now()}
	local := session.New(context.Background(), nil, nil, data)
	if err := n1.reg.Register(local); err != nil {
		t.Fatal(err)
	}
	n1.mesh.ForcePeerDeadImmediate("n2")
	time.Sleep(50 * time.Millisecond)
	if _, _, ok := n1.mesh.Lookup("ONN2"); ok {
		t.Fatal("directory should drop")
	}
}

func TestE2E_ClusterHighLatencyMesh(t *testing.T) {
	h := startMultiNode(t, "n1", "n2", "n3")
	h.hub.SetDelay(60 * time.Millisecond)
	n1, n2 := h.node("n1"), h.node("n2")
	_ = loginLocal(t, n1, "P1", 1, false)
	_ = loginLocal(t, n2, "P2", 2, false)
	// Publish interest covering both
	n2.mesh.PublishInterest(cluster.InterestSummary{
		NodeID: "n2",
		Boxes:  []cluster.AABB{{MinLat: -90, MaxLat: 90, MinLon: -180, MaxLon: 180}},
	})
	var got int32
	n2.mesh.OnPositionBatch(func(_ string, batch cluster.PositionBatch) {
		atomic.AddInt32(&got, 1)
	})
	n1.mesh.ForwardRanged([]byte("@P1:...\r\n"), []cluster.AABB{{MinLat: 0, MaxLat: 1, MinLon: 0, MaxLon: 1}}, cluster.RangeClassPosition, "P1")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&got) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no position under latency")
}

func TestE2E_ClusterThreeNodeSFHints(t *testing.T) {
	h := startMultiNode(t, "n1", "n2", "n3")
	n1 := h.node("n1")
	// Pilot on n1
	pilot := loginLocal(t, n1, "SFP1", 5, false)
	// Inject multi-peer proximity hints
	n1.reg.onProximityHint("n2", "SFP1", 2*1852) // 2 NM
	n1.reg.onProximityHint("n3", "SFP1", 8*1852) // 8 NM
	min := n1.reg.RemoteClosestM("SFP1", time.Now())
	if min > 3*1852 {
		t.Fatalf("min=%v want ~2NM", min)
	}
	// After expiry of n2 only, min should rise
	n1.reg.hintMu.Lock()
	n1.reg.hintByCS["SFP1"]["n2"] = remoteHint{distanceM: 2 * 1852, expiry: time.Now().Add(-time.Second)}
	n1.reg.hintMu.Unlock()
	min = n1.reg.RemoteClosestM("SFP1", time.Now())
	if min < 7*1852 {
		t.Fatalf("min=%v after n2 expire", min)
	}
	_ = pilot
}
