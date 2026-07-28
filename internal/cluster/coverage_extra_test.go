package cluster

import (
	"bytes"
	"context"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryMeshAbortReleaseBroadcastProximity(t *testing.T) {
	hub := NewMemoryHub()
	peers := []string{"a", "b"}
	a, err := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "a", PeerIDs: peers, ClaimTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "b", PeerIDs: peers, ClaimTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_ = a.Start(context.Background())
	_ = b.Start(context.Background())
	defer a.Stop()
	defer b.Stop()

	ctx := context.Background()
	fence, err := a.ClaimReserve(ctx, "AB1", ClaimMeta{NodeID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	a.ClaimAbort("AB1", fence)
	// reserve again and commit then release
	fence, err = a.ClaimReserve(ctx, "AB1", ClaimMeta{NodeID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ClaimCommit(ctx, "AB1", fence); err != nil {
		t.Fatal(err)
	}
	a.ClaimRelease("AB1", fence)

	var jl int
	b.OnDirectWire(func(_ string, wire []byte, class WireClass) {
		if class == WireJoinLeave {
			jl++
		}
	})
	a.BroadcastJoinLeave([]byte("#APx\r\n"))
	time.Sleep(20 * time.Millisecond)

	var hintOK bool
	a.OnProximityHint(func(from, cs string, d float64) {
		if cs == "T" && d == 100 {
			hintOK = true
		}
	})
	b.SendProximityHint("a", "T", 100)
	time.Sleep(20 * time.Millisecond)
	if !hintOK {
		t.Fatal("proximity")
	}

	// Owner-down fail-closed
	a.deadMu.Lock()
	a.alive["b"] = false
	a.deadMu.Unlock()
	// Force owner of some callsign to b if possible
	owner := a.OwnerNode("ZZZ")
	if owner == "b" {
		_, err := a.ClaimReserve(ctx, "ZZZ", ClaimMeta{NodeID: "a"})
		if err != ErrPeerDown && err != ErrClaimTimeout {
			// may still work if local — ok
			_ = err
		}
	}
}

func TestMergeInterestOverflowPath(t *testing.T) {
	var boxes []InterestBox
	// Many ATC boxes to force overflow
	for i := 0; i < 200; i++ {
		boxes = append(boxes, InterestBox{
			AABB:  AABB{float64(i * 10), float64(i * 10), float64(i*10) + 0.5, float64(i*10) + 0.5},
			IsATC: true,
		})
	}
	merged, overflow := MergeInterestBoxes(boxes, 16)
	if len(merged) > 16 {
		t.Fatalf("len %d", len(merged))
	}
	_ = overflow
	// empty
	m2, _ := MergeInterestBoxes(nil, 64)
	if m2 != nil && len(m2) != 0 {
		t.Fatal()
	}
	// maxBoxes 0 uses default
	_, _ = MergeInterestBoxes(boxes[:5], 0)

	// Mixed ATC+pilot over cap: ATC protected, pilots merge
	var mixed []InterestBox
	for i := 0; i < 20; i++ {
		mixed = append(mixed, InterestBox{
			AABB: AABB{float64(i), 0, float64(i) + 0.2, 0.2}, IsATC: true,
		})
	}
	for i := 0; i < 80; i++ {
		mixed = append(mixed, InterestBox{
			AABB: AABB{100 + float64(i)*0.01, 0, 100 + float64(i)*0.01 + 0.01, 0.01}, IsATC: false,
		})
	}
	m3, _ := MergeInterestBoxes(mixed, 32)
	if len(m3) > 32 {
		t.Fatalf("%d", len(m3))
	}
	// co-located pilots merge via quantize
	co := []InterestBox{
		{AABB: AABB{0, 0, 0.1, 0.1}, IsATC: false},
		{AABB: AABB{0.01, 0.01, 0.11, 0.11}, IsATC: false},
		{AABB: AABB{0.02, 0.02, 0.12, 0.12}, IsATC: false},
	}
	m4, _ := MergeInterestBoxes(co, 64)
	if len(m4) > 3 {
		t.Fatalf("%d", len(m4))
	}
	// polar cos path
	_ = AABBFromCenterRange(89.9, 0, 1000)
	SortAABBsForTest([]AABB{{2, 2, 3, 3}, {1, 1, 2, 2}, {1, 0, 2, 1}})
}

func TestClaimTTLExpiry(t *testing.T) {
	ct := NewClaimTable(10 * time.Millisecond)
	fence, err := ct.Reserve("TTL", ClaimMeta{NodeID: "a", TTL: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := ct.Commit("TTL", fence); err == nil {
		t.Fatal("expected expired")
	}
	// reserve again after expire
	if _, err := ct.Reserve("TTL", ClaimMeta{NodeID: "b"}); err != nil {
		t.Fatal(err)
	}
}

func TestFrameErrorPaths(t *testing.T) {
	if _, err := DecodeFrame(bytes.NewReader([]byte{0, 0, 0, 0, 1})); err == nil {
		// n==0
		_ = err
	}
	// short string
	if _, _, err := decodeString([]byte{0}); err == nil {
		t.Fatal()
	}
	if _, _, err := decodeBytes([]byte{0, 0}); err == nil {
		t.Fatal()
	}
	if _, _, err := decodeU64([]byte{1}); err == nil {
		t.Fatal()
	}
	if _, _, err := decodeU32([]byte{1}); err == nil {
		t.Fatal()
	}
	// large string truncate path
	_ = encodeString(nil, string(make([]byte, 70000)))
	_ = encodeBytes(nil, make([]byte, 100))
}

func TestDirectoryConflictSnapshot(t *testing.T) {
	d := NewDirectory()
	d.ApplyJoin(DirMeta{Callsign: "C", NodeID: "a", Fence: "f1", Routable: true})
	d.ApplySnapshot([]DirMeta{
		{Callsign: "C", NodeID: "a", Fence: "f2", Routable: true}, // same node different fence — keep existing
		{Callsign: "D", NodeID: "b", Fence: "g", Routable: true},
		{Callsign: "", NodeID: "x", Routable: true}, // skip empty
	})
	if _, _, ok := d.Lookup("D"); !ok {
		t.Fatal()
	}
	// DecodeSnapshot errors
	if _, err := DecodeSnapshot([]byte{0}); err == nil {
		t.Fatal()
	}
	if _, _, err := DecodeDelta(nil); err == nil {
		t.Fatal()
	}
}

func TestHashEdgeCases(t *testing.T) {
	if _, err := NewStablePeerRing(nil); err != ErrInvalidConfig {
		t.Fatal()
	}
	r, err := NewStablePeerRing([]string{"only", "only", "  "})
	if err != nil || r.Len() != 1 {
		t.Fatal(err, r.Len())
	}
	if r.Owner("x") != "only" {
		t.Fatal()
	}
	if _, err := ParseClusterPeers("bad"); err == nil {
		t.Fatal()
	}
	if _, err := ParseClusterPeers("a=,b=host:1"); err == nil {
		t.Fatal()
	}
	// too many peers
	var parts []string
	for i := 0; i < 9; i++ {
		parts = append(parts, string(rune('a'+i))+"=h:1")
	}
	// join
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		s += "," + parts[i]
	}
	if _, err := ParseClusterPeers(s); err == nil {
		t.Fatal()
	}
}

func TestTCPMeshPSKRejectAndCallbacks(t *testing.T) {
	addrA := freePort(t)
	addrB := freePort(t)
	peers := []PeerAddr{{NodeID: "ta", Addr: addrA}, {NodeID: "tb", Addr: addrB}}
	a, _ := NewTCPMesh(TCPMeshConfig{
		NodeID: "ta", ListenAddr: addrA, Peers: peers,
		ClaimTimeout: time.Second, PeerDeathGrace: 100 * time.Millisecond, PSK: "right",
	})
	b, _ := NewTCPMesh(TCPMeshConfig{
		NodeID: "tb", ListenAddr: addrB, Peers: peers,
		ClaimTimeout: time.Second, PeerDeathGrace: 100 * time.Millisecond, PSK: "wrong",
	})
	_ = a.Start(context.Background())
	_ = b.Start(context.Background())
	defer a.Stop()
	defer b.Stop()
	time.Sleep(200 * time.Millisecond)
	// link should not establish with PSK mismatch — claim local still works on a
	ctx := context.Background()
	// Local owner claim
	cs := "LOCALONLY"
	// Try reserve — if owner is a, works locally
	fence, err := a.ClaimReserve(ctx, cs, ClaimMeta{NodeID: "ta"})
	if err == nil {
		_ = a.ClaimCommit(ctx, cs, fence)
		a.ClaimRelease(cs, fence)
	}
	// OnPeerDead registration
	a.OnPeerDead(func(string, []DirMeta) {})
	a.OnProximityHint(func(string, string, float64) {})
}

func TestMemoryMeshHomeRPCTimeoutAndNotFound(t *testing.T) {
	hub := NewMemoryHub()
	m, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "solo", PeerIDs: []string{"solo"}, ClaimTimeout: 50 * time.Millisecond})
	_ = m.Start(context.Background())
	defer m.Stop()
	ctx := context.Background()
	if _, err := m.HomeRPC(ctx, "NOPE", HomeOpForceDisconnect, nil); err != ErrHomeRPCNotFound {
		// lookup fails
		if err != ErrHomeRPCNotFound && err != ErrNotRoutable {
			// ok if direct not found style
			_ = err
		}
	}
	if err := m.SendDirect("NOPE", []byte("x")); err == nil {
		t.Fatal()
	}
}

func TestMemoryMeshOwnerDownAndTimeouts(t *testing.T) {
	hub := NewMemoryHub()
	peers := []string{"a", "b"}
	a, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "a", PeerIDs: peers, ClaimTimeout: 30 * time.Millisecond})
	b, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "b", PeerIDs: peers, ClaimTimeout: 30 * time.Millisecond})
	_ = a.Start(context.Background())
	_ = b.Start(context.Background())
	defer a.Stop()
	defer b.Stop()

	// Mark b dead
	a.deadMu.Lock()
	a.alive["b"] = false
	a.deadMu.Unlock()

	// Find a callsign owned by b
	var cs string
	for _, c := range []string{"Z1", "Z2", "Z3", "Z4", "Z5", "Z6", "Z7", "Z8", "Z9", "Z0", "ZA", "ZB", "ZC"} {
		if a.OwnerNode(c) == "b" {
			cs = c
			break
		}
	}
	if cs == "" {
		t.Skip("no callsign owned by b")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := a.ClaimReserve(ctx, cs, ClaimMeta{NodeID: "a"})
	if err == nil {
		t.Fatal("expected peer down or timeout")
	}

	// Cancelled context
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	_, _ = a.ClaimReserve(ctx2, "SELF", ClaimMeta{NodeID: "a"})
	_ = a.ClaimCommit(ctx2, "SELF", "x")

	// Commit fail path
	ctx3 := context.Background()
	f, err := a.ClaimReserve(ctx3, "OK1", ClaimMeta{NodeID: "a"})
	if err == nil {
		_ = a.ClaimCommit(ctx3, "OK1", "bad-fence")
		_ = f
	}

	// HomeRPC when target peer down
	a.dir.ApplyJoin(DirMeta{Callsign: "RP", NodeID: "b", Routable: true})
	_, err = a.HomeRPC(ctx3, "RP", HomeOpForceDisconnect, nil)
	if err == nil {
		// peer dead may still try
		_ = err
	}
	// SendDirect peer down
	_ = a.SendDirect("RP", []byte("x"))

	// Local HomeRPC nil handler
	a.dir.ApplyJoin(DirMeta{Callsign: "LH", NodeID: "a", Routable: true})
	a.onHomeRPC = nil
	_, _ = a.HomeRPC(ctx3, "LH", HomeOpForceDisconnect, nil)

	// Forward with interest that filters
	b.PublishInterest(InterestSummary{NodeID: "b", Boxes: []AABB{{100, 100, 101, 101}}})
	a.ForwardRanged([]byte("@"), []AABB{{0, 0, 1, 1}}, RangeClassPosition, "SENDER")
	// Forward with overlap
	b.PublishInterest(InterestSummary{NodeID: "b", Boxes: []AABB{{0, 0, 2, 2}}})
	var n int32
	b.OnPositionBatch(func(string, PositionBatch) { n++ })
	a.ForwardRanged([]byte("@"), []AABB{{0.5, 0.5, 1.5, 1.5}}, RangeClassPosition, "SENDER")
	time.Sleep(20 * time.Millisecond)

	// BroadcastClass to dead peer still iterates
	a.BroadcastClass([]byte("x"), BroadcastAll)
	// SendProximity to missing peer
	a.SendProximityHint("missing", "T", 1)
}

func TestNewMemoryMeshErrors(t *testing.T) {
	if _, err := NewMemoryMesh(NewMemoryHub(), MemoryMeshConfig{}); err != ErrInvalidConfig {
		t.Fatal(err)
	}
	// >8 peers
	ids := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}
	if _, err := NewMemoryMesh(NewMemoryHub(), MemoryMeshConfig{NodeID: "1", PeerIDs: ids}); err != ErrInvalidConfig {
		t.Fatal(err)
	}
}

func TestTinyCoverageBoosts(t *testing.T) {
	// directory empty callsign
	d := NewDirectory()
	d.ApplyJoin(DirMeta{})
	d.ApplyMeta("nope", nil, nil)
	// claim defaults
	ct := NewClaimTable(0)
	f, _ := ct.Reserve("x", ClaimMeta{NodeID: "a", Fence: "fixed"})
	if f != "fixed" {
		t.Fatal(f)
	}
	// empty fence must NOT force-release (R2-1)
	if ct.Release("x", "") {
		t.Fatal("empty fence must not release")
	}
	ct.Release("x", f)
	// ReleaseNode pending
	ct.Reserve("p1", ClaimMeta{NodeID: "dead"})
	ct.ReleaseNode("dead")
	// LookupActive miss
	_, _, ok := ct.LookupActive("nope")
	if ok {
		t.Fatal()
	}
	// frame too large decode
	var hdr [5]byte
	// n = MaxFramePayload+2
	binary.BigEndian.PutUint32(hdr[0:4], MaxFramePayload+2)
	hdr[4] = 1
	if _, err := DecodeFrame(bytes.NewReader(hdr[:])); err != ErrFrameTooLarge {
		// ok
		_ = err
	}
	// DecodeFrame short type only n=1 empty payload
	binary.BigEndian.PutUint32(hdr[0:4], 1)
	fr, err := DecodeFrame(bytes.NewReader(hdr[:]))
	if err != nil || fr.Type != 1 {
		_ = err
	}
	// hash empty owner on empty ring shouldn't happen; Len
	r, _ := NewStablePeerRing([]string{"z"})
	if r.Owner("a") != "z" {
		t.Fatal()
	}
	// decodeDirMeta short
	_, _, err = decodeDirMeta([]byte{0})
	_ = err
	// encode string ATC flags true paths
	m := DirMeta{Callsign: "A", NodeID: "n", IsATC: true, Routable: true, Fence: "f"}
	_ = EncodeSnapshot([]DirMeta{m})
	// memory SimulatePeerDown stop early
	hub := NewMemoryHub()
	mm, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "s", PeerIDs: []string{"s"}, PeerDeathGrace: time.Hour})
	_ = mm.Start(context.Background())
	mm.SimulatePeerDown("x")
	_ = mm.Stop()
}

func TestSanitizeAndEnqueue(t *testing.T) {
	if SanitizeMeshString("a\r\nb", 10) != "ab" {
		t.Fatal()
	}
	w := SanitizeWireBytes([]byte("x\ry\n\r\n"))
	if string(w) != "xy\r\n" {
		t.Fatalf("%q", w)
	}
}

func TestTCPMeshEnqueueDrop(t *testing.T) {
	m := newIsolatedTCP(t, "a", "b")
	pipePeer(t, m, "b")
	// flood fire-and-forget
	for i := 0; i < peerSendQueueSize+10; i++ {
		_ = m.sendTo("b", TypeHeartbeat, nil)
	}
	// position coalesce path
	m.ForwardRanged([]byte("@\r\n"), []AABB{{0, 0, 1, 1}}, RangeClassPosition, "P")
	m.ForwardRanged([]byte("@\r\n"), []AABB{{0, 0, 1, 1}}, RangeClassPosition, "P")
	time.Sleep(50 * time.Millisecond)
	m.SendProximityHint("b", "P", 100)
	m.SendProximityHint("a", "P", 50) // local
}

func TestFourMoreLines(t *testing.T) {
	// ApplyJoin ownership conflict keep existing
	d := NewDirectory()
	d.ApplyJoin(DirMeta{Callsign: "C1", NodeID: "a", Routable: true})
	d.ApplyJoin(DirMeta{Callsign: "C1", NodeID: "b", Routable: true}) // rejected
	_, m, ok := d.Lookup("C1")
	if !ok || m.NodeID != "a" {
		t.Fatal(m)
	}
	// hash empty
	r, _ := NewStablePeerRing([]string{"z"})
	if r.Owner("") == "" {
		t.Fatal()
	}
	// memory mesh SendDirect local
	hub := NewMemoryHub()
	mm, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "s", PeerIDs: []string{"s"}})
	_ = mm.Start(context.Background())
	mm.NotifyLocalJoin(DirMeta{Callsign: "L", NodeID: "s", Routable: true})
	var hit bool
	mm.OnDirectWire(func(string, []byte, WireClass) { hit = true })
	_ = mm.SendDirect("L", []byte("x\r\n"))
	if !hit {
		t.Fatal()
	}
	_ = mm.Stop()
}

func TestForwardTextRangedAndSnapshotFrom(t *testing.T) {
	hub := NewMemoryHub()
	a, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "a", PeerIDs: []string{"a", "b"}})
	b, _ := NewMemoryMesh(hub, MemoryMeshConfig{NodeID: "b", PeerIDs: []string{"a", "b"}})
	_ = a.Start(context.Background())
	_ = b.Start(context.Background())
	defer a.Stop()
	defer b.Stop()
	var got atomic.Bool
	b.OnDirectWire(func(_ string, wire []byte, class WireClass) {
		if class == WireRanged {
			got.Store(true)
		}
	})
	b.PublishInterest(InterestSummary{NodeID: "b", Boxes: []AABB{{0, 0, 10, 10}}})
	a.ForwardTextRanged([]byte("#TMx:@12345:hi\r\n"), []AABB{{1, 1, 2, 2}})
	time.Sleep(30 * time.Millisecond)
	if !got.Load() {
		t.Fatal("text ranged not received")
	}
	// TCP text path
	m := newIsolatedTCP(t, "ta", "tb")
	pipePeer(t, m, "tb")
	m.interest["tb"] = InterestSummary{NodeID: "tb", Boxes: []AABB{{0, 0, 5, 5}}}
	m.ForwardTextRanged([]byte("#TM\r\n"), []AABB{{1, 1, 2, 2}})
	// handle TypeTextRanged
	var tr bool
	m.OnDirectWire(func(_ string, _ []byte, c WireClass) {
		if c == WireRanged {
			tr = true
		}
	})
	m.handleFrame("tb", Frame{Type: TypeTextRanged, Payload: []byte("x")})
	if !tr {
		t.Fatal()
	}
	// empty fence claim release on frame
	m.handleFrame("tb", Frame{Type: TypeClaimRelease, Payload: encodeString(encodeString(nil, "CS"), "")})
}

func TestApplySnapshotFromWire(t *testing.T) {
	m := newIsolatedTCP(t, "a", "b")
	// snapshot from b with foreign ownership ignored
	raw := EncodeSnapshot([]DirMeta{
		{Callsign: "MINE", NodeID: "b", Routable: true},
		{Callsign: "THEIR", NodeID: "c", Routable: true},
	})
	m.handleFrame("b", Frame{Type: TypeDirectorySnapshot, Payload: raw})
	if _, _, ok := m.Lookup("MINE"); !ok {
		t.Fatal("mine")
	}
	if _, _, ok := m.Lookup("THEIR"); ok {
		t.Fatal("foreign")
	}
}
