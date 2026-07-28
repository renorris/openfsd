package cluster

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func startTwoTCP(t *testing.T) (a, b *TCPMesh) {
	t.Helper()
	addrA := freePort(t)
	addrB := freePort(t)
	peers := []PeerAddr{
		{NodeID: "ta", Addr: addrA},
		{NodeID: "tb", Addr: addrB},
	}
	var err error
	// Start lower ID first so accept is ready when higher ID dials.
	a, err = NewTCPMesh(TCPMeshConfig{
		NodeID: "ta", ListenAddr: addrA, Peers: peers,
		ClaimTimeout: 2 * time.Second, PeerDeathGrace: 500 * time.Millisecond,
		PSK: "test-psk",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err = NewTCPMesh(TCPMeshConfig{
		NodeID: "tb", ListenAddr: addrB, Peers: peers,
		ClaimTimeout: 2 * time.Second, PeerDeathGrace: 500 * time.Millisecond,
		PSK: "test-psk",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// Wait for bidirectional readiness: both see a conn.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.RLock()
		na := len(a.conns)
		a.mu.RUnlock()
		b.mu.RLock()
		nb := len(b.conns)
		b.mu.RUnlock()
		if na > 0 && nb > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.mu.RLock()
	na := len(a.conns)
	a.mu.RUnlock()
	if na == 0 {
		t.Fatal("mesh link did not establish")
	}
	t.Cleanup(func() { _ = a.Stop(); _ = b.Stop() })
	return a, b
}

func TestTCPMeshClaimAndLookup(t *testing.T) {
	a, b := startTwoTCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Owner may be a or b depending on hash
	fence, err := a.ClaimReserve(ctx, "TCP1", ClaimMeta{NodeID: "ta", CID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClaimCommit(ctx, "TCP1", fence); err != nil {
		t.Fatal(err)
	}
	// Directory may need a moment to propagate
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, ok := b.Lookup("TCP1"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, _, ok := b.Lookup("TCP1"); !ok {
		t.Fatal("b directory miss")
	}
	// Concurrent claim should fail
	if _, err := b.ClaimReserve(ctx, "TCP1", ClaimMeta{NodeID: "tb"}); err == nil {
		t.Fatal("expected in use")
	}
	a.ClaimRelease("TCP1", fence)
	time.Sleep(50 * time.Millisecond)
}

func TestTCPMeshHomeRPCAndDirect(t *testing.T) {
	a, b := startTwoTCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Register handlers before claim so late packets are not dropped.
	var fpl string
	a.OnHomeRPC(func(op HomeOp, cs string, payload []byte) ([]byte, error) {
		switch op {
		case HomeOpMutateFlightPlan:
			fpl = string(payload)
			return nil, nil
		case HomeOpQuerySessionMeta:
			return append([]byte(fpl), 0), nil
		case HomeOpAssignBeacon, HomeOpForceDisconnect:
			return nil, nil
		default:
			return nil, ErrHomeRPCNotFound
		}
	})
	var gotDirect atomic.Bool
	a.OnDirectWire(func(_ string, wire []byte, class WireClass) {
		if class == WireDirect {
			gotDirect.Store(true)
		}
	})

	fence, err := a.ClaimReserve(ctx, "HOME1", ClaimMeta{NodeID: "ta"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClaimCommit(ctx, "HOME1", fence); err != nil {
		t.Fatal(err)
	}
	// Wait directory on b
	ok := false
	for i := 0; i < 100; i++ {
		if _, _, ok = b.Lookup("HOME1"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ok {
		t.Fatal("directory not propagated")
	}

	if _, err := b.HomeRPC(ctx, "HOME1", HomeOpMutateFlightPlan, []byte("PLAN")); err != nil {
		t.Fatal(err)
	}
	if fpl != "PLAN" {
		t.Fatalf("fpl=%q", fpl)
	}
	if err := b.SendDirect("HOME1", []byte("#TMx:HOME1:hi\r\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !gotDirect.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if !gotDirect.Load() {
		t.Fatal("direct not received")
	}
}

func TestTCPMeshInterestAndPosition(t *testing.T) {
	a, b := startTwoTCP(t)
	// wait link
	time.Sleep(100 * time.Millisecond)
	b.PublishInterest(InterestSummary{
		NodeID: "tb",
		Boxes:  []AABB{{MinLat: 0, MaxLat: 10, MinLon: 0, MaxLon: 10}},
	})
	var got atomic.Int32
	b.OnPositionBatch(func(_ string, batch PositionBatch) {
		got.Add(1)
	})
	// allow interest propagate
	time.Sleep(50 * time.Millisecond)
	a.ForwardRanged([]byte("@\r\n"), []AABB{{MinLat: 1, MaxLat: 2, MinLon: 1, MaxLon: 2}}, RangeClassPosition, "SENDER")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no position batch")
}

func TestTCPMeshBroadcastJoinLeave(t *testing.T) {
	a, b := startTwoTCP(t)
	var got atomic.Bool
	b.OnDirectWire(func(_ string, wire []byte, class WireClass) {
		if class == WireJoinLeave {
			got.Store(true)
		}
	})
	time.Sleep(50 * time.Millisecond)
	a.BroadcastJoinLeave([]byte("#APX:SERVER:1\r\n"))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !got.Load() {
		time.Sleep(10 * time.Millisecond)
	}
	if !got.Load() {
		t.Fatal("join not received")
	}
	// also exercise BroadcastClass / NotifyLocal*
	a.BroadcastClass([]byte("*S\r\n"), BroadcastSupervisor)
	a.NotifyLocalJoin(DirMeta{Callsign: "NJ", NodeID: "ta", Routable: true})
	a.NotifyLocalFPL("NJ", "VFR")
	a.NotifyLocalBeacon("NJ", "1200")
	a.NotifyLocalLeave("NJ")
	_ = a.NodeID()
	_ = a.PeerIDs()
	_ = a.OwnerNode("NJ")
	_ = a.PeerDeathGrace()
	_ = a.ClaimTimeout()
	a.ClaimAbort("nope", "f")
}

func TestTCPMeshEncodePositionBatchRoundTrip(t *testing.T) {
	b := PositionBatch{
		SenderCallsign:   "P",
		SenderBoxes:      []AABB{{1, 2, 3, 4}},
		Velocity:         true,
		Wires:            [][]byte{[]byte("x")},
		ClosestDistanceM: 12.5,
	}
	raw := encodePositionBatch(b)
	out, err := decodePositionBatch(raw)
	if err != nil || out.SenderCallsign != "P" || !out.Velocity || len(out.Wires) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	sum := InterestSummary{NodeID: "n", Boxes: []AABB{{0, 0, 1, 1}}}
	raw2 := encodeInterest(sum)
	sum2, err := decodeInterest(raw2)
	if err != nil || sum2.NodeID != "n" || len(sum2.Boxes) != 1 {
		t.Fatalf("%+v %v", sum2, err)
	}
}

func TestNewTCPMeshValidation(t *testing.T) {
	if _, err := NewTCPMesh(TCPMeshConfig{}); err != ErrInvalidConfig {
		t.Fatal(err)
	}
}

func TestMemoryMeshAccessors(t *testing.T) {
	hub := NewMemoryHub()
	m, err := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "x", PeerIDs: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.NodeID() != "x" || len(m.PeerIDs()) != 1 {
		t.Fatal()
	}
	if m.PeerDeathGrace() <= 0 || m.ClaimTimeout() <= 0 {
		t.Fatal()
	}
	m.BroadcastClass([]byte("x"), BroadcastAll)
	m.NotifyLocalJoin(DirMeta{Callsign: "Z", NodeID: "x"})
	m.NotifyLocalFPL("Z", "fp")
	m.NotifyLocalBeacon("Z", "1")
	m.NotifyLocalLeave("Z")
	_ = NewFence()
	_ = MinFloat(1, 2)
	r, _ := NewStablePeerRing([]string{"a"})
	_ = r.Len()
	_ = r.IDs()
	ct := NewClaimTable(time.Second)
	f, _ := ct.Reserve("L", ClaimMeta{NodeID: "a"})
	_, _ = ct.Commit("L", f)
	_, _, _ = ct.LookupActive("L")
	SortAABBsForTest([]AABB{{1, 1, 2, 2}, {0, 0, 1, 1}})
	_ = filterToAABB([]InterestBox{{AABB: AABB{0, 0, 1, 1}}})
}
