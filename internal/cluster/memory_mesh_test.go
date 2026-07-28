package cluster

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func twoNodeMesh(t *testing.T) (hub *MemoryHub, a, b *MemoryMesh) {
	t.Helper()
	hub = NewMemoryHub()
	peers := []string{"a", "b"}
	var err error
	a, err = NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "a", PeerIDs: peers, ClaimTimeout: time.Second, PeerDeathGrace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	b, err = NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "b", PeerIDs: peers, ClaimTimeout: time.Second, PeerDeathGrace: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(); _ = b.Stop() })
	return hub, a, b
}

func TestMemoryMeshConcurrentClaim(t *testing.T) {
	_, a, b := twoNodeMesh(t)
	// Same callsign from both nodes — exactly one wins
	var okA, okB atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		fence, err := a.ClaimReserve(ctx, "SAME", ClaimMeta{NodeID: "a", CID: 1})
		if err != nil {
			return
		}
		if err := a.ClaimCommit(ctx, "SAME", fence); err != nil {
			a.ClaimAbort("SAME", fence)
			return
		}
		okA.Store(true)
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		fence, err := b.ClaimReserve(ctx, "SAME", ClaimMeta{NodeID: "b", CID: 2})
		if err != nil {
			return
		}
		if err := b.ClaimCommit(ctx, "SAME", fence); err != nil {
			b.ClaimAbort("SAME", fence)
			return
		}
		okB.Store(true)
	}()
	wg.Wait()
	wins := 0
	if okA.Load() {
		wins++
	}
	if okB.Load() {
		wins++
	}
	if wins != 1 {
		t.Fatalf("wins=%d want exactly 1", wins)
	}
	// Directory routable on both
	_, _, ok1 := a.Lookup("SAME")
	_, _, ok2 := b.Lookup("SAME")
	if !ok1 || !ok2 {
		t.Fatalf("directory a=%v b=%v", ok1, ok2)
	}
}

func TestMemoryMeshHomeRPC(t *testing.T) {
	_, a, b := twoNodeMesh(t)
	ctx := context.Background()
	fence, err := a.ClaimReserve(ctx, "TGT", ClaimMeta{NodeID: "a", CID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClaimCommit(ctx, "TGT", fence); err != nil {
		t.Fatal(err)
	}
	// Home on a
	var gotFP string
	a.OnHomeRPC(func(op HomeOp, cs string, payload []byte) ([]byte, error) {
		if op == HomeOpMutateFlightPlan {
			gotFP = string(payload)
			return nil, nil
		}
		if op == HomeOpQuerySessionMeta {
			return append(append([]byte(gotFP), 0), []byte("1200")...), nil
		}
		if op == HomeOpForceDisconnect {
			return nil, nil
		}
		if op == HomeOpAssignBeacon {
			return nil, nil
		}
		return nil, ErrHomeRPCNotFound
	})
	_, err = b.HomeRPC(ctx, "TGT", HomeOpMutateFlightPlan, []byte("VFR"))
	if err != nil {
		t.Fatal(err)
	}
	if gotFP != "VFR" {
		t.Fatalf("got %q", gotFP)
	}
	resp, err := b.HomeRPC(ctx, "TGT", HomeOpQuerySessionMeta, nil)
	if err != nil || string(resp[:3]) != "VFR" {
		t.Fatalf("%q %v", resp, err)
	}
}

func TestMemoryMeshDirectAndJoin(t *testing.T) {
	_, a, b := twoNodeMesh(t)
	ctx := context.Background()
	fence, _ := a.ClaimReserve(ctx, "P1", ClaimMeta{NodeID: "a"})
	_ = a.ClaimCommit(ctx, "P1", fence)

	var got []byte
	var mu sync.Mutex
	a.OnDirectWire(func(_ string, wire []byte, class WireClass) {
		mu.Lock()
		got = append([]byte(nil), wire...)
		mu.Unlock()
	})
	if err := b.SendDirect("P1", []byte("#TMx:P1:hi\r\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if string(got) == "" {
		t.Fatal("no direct")
	}
	mu.Unlock()
}

func TestMemoryMeshPeerDeath(t *testing.T) {
	_, a, b := twoNodeMesh(t)
	ctx := context.Background()
	fence, _ := b.ClaimReserve(ctx, "DEAD1", ClaimMeta{NodeID: "b"})
	_ = b.ClaimCommit(ctx, "DEAD1", fence)

	var deadCS []string
	var mu sync.Mutex
	a.OnPeerDead(func(nodeID string, metas []DirMeta) {
		mu.Lock()
		for _, m := range metas {
			deadCS = append(deadCS, m.Callsign)
		}
		mu.Unlock()
	})
	a.ForcePeerDeadImmediate("b")
	time.Sleep(30 * time.Millisecond)
	if _, _, ok := a.Lookup("DEAD1"); ok {
		t.Fatal("should be removed from directory")
	}
}

func TestMemoryMeshHighLatencyClaim(t *testing.T) {
	hub, a, b := twoNodeMesh(t)
	hub.SetDelay(80 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	fence, err := a.ClaimReserve(ctx, "LAT", ClaimMeta{NodeID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClaimCommit(ctx, "LAT", fence); err != nil {
		t.Fatal(err)
	}
	// other node cannot claim
	if _, err := b.ClaimReserve(ctx, "LAT", ClaimMeta{NodeID: "b"}); err == nil {
		t.Fatal("expected in use")
	}
}

func TestMemoryMeshInterestForward(t *testing.T) {
	_, a, b := twoNodeMesh(t)
	b.PublishInterest(InterestSummary{
		NodeID: "b",
		Boxes:  []AABB{{MinLat: 40, MaxLat: 41, MinLon: -74, MaxLon: -73}},
	})
	var got int32
	b.OnPositionBatch(func(_ string, batch PositionBatch) {
		atomic.AddInt32(&got, 1)
	})
	// sender near NYC
	boxes := []AABB{{MinLat: 40.5, MaxLat: 40.6, MinLon: -73.9, MaxLon: -73.8}}
	a.ForwardRanged([]byte("@x\r\n"), boxes, RangeClassPosition, "SENDER")
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&got) < 1 {
		t.Fatal("expected position batch")
	}
}

func TestOwnerNodeSelf(t *testing.T) {
	_, a, _ := twoNodeMesh(t)
	owner := a.OwnerNode("anything")
	if owner != "a" && owner != "b" {
		t.Fatal(owner)
	}
}
