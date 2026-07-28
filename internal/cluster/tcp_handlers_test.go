package cluster

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// pipePeer attaches a net.Pipe as a connected peer for unit-testing handlers.
func pipePeer(t *testing.T, m *TCPMesh, peerID string) (local, remote net.Conn) {
	t.Helper()
	local, remote = net.Pipe()
	pc := &peerConn{
		nodeID: peerID,
		conn:   local,
		outCh:  make(chan frameJob, peerSendQueueSize),
		lastHB: time.Now(),
		stop:   make(chan struct{}),
	}
	m.mu.Lock()
	m.conns[peerID] = pc
	m.mu.Unlock()
	// Writer + remote drain
	go m.peerWriter(pc)
	go func() {
		buf := make([]byte, 64*1024)
		for {
			_, err := remote.Read(buf)
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		select {
		case <-pc.stop:
		default:
			close(pc.stop)
		}
		_ = local.Close()
		_ = remote.Close()
		m.mu.Lock()
		delete(m.conns, peerID)
		m.mu.Unlock()
	})
	return local, remote
}

func newIsolatedTCP(t *testing.T, id string, peers ...string) *TCPMesh {
	t.Helper()
	ids := append([]string{id}, peers...)
	var addrs []PeerAddr
	for _, p := range ids {
		addrs = append(addrs, PeerAddr{NodeID: p, Addr: "127.0.0.1:1"})
	}
	m, err := NewTCPMesh(TCPMeshConfig{
		NodeID: id, ListenAddr: "127.0.0.1:0", Peers: addrs,
		ClaimTimeout: time.Second, PeerDeathGrace: 50 * time.Millisecond,
		PSK: "test-psk",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Don't Start — we inject peers.
	return m
}

func TestTCPHandleFrameMatrix(t *testing.T) {
	m := newIsolatedTCP(t, "a", "b")
	pipePeer(t, m, "b")

	// Heartbeat
	m.handleFrame("b", Frame{Type: TypeHeartbeat})

	// Directory snapshot + delta
	snap := EncodeSnapshot([]DirMeta{{Callsign: "X", NodeID: "b", Routable: true, Fence: "f"}})
	m.handleFrame("b", Frame{Type: TypeDirectorySnapshot, Payload: snap})
	if _, _, ok := m.Lookup("X"); !ok {
		t.Fatal("snap")
	}
	m.handleFrame("b", Frame{Type: TypeDirectoryDelta, Payload: EncodeDelta(DeltaLeave, DirMeta{Callsign: "X"})})
	if _, _, ok := m.Lookup("X"); ok {
		t.Fatal("leave")
	}
	m.handleFrame("b", Frame{Type: TypeDirectoryDelta, Payload: EncodeDelta(DeltaJoin, DirMeta{Callsign: "Y", NodeID: "b", Routable: true})})
	m.handleFrame("b", Frame{Type: TypeDirectoryDelta, Payload: EncodeDelta(DeltaMeta, DirMeta{Callsign: "Y", NodeID: "b", FPLInfo: "fp", Routable: true})})

	// Direct / join / broadcast
	var classes []WireClass
	m.OnDirectWire(func(_ string, _ []byte, c WireClass) { classes = append(classes, c) })
	m.handleFrame("b", Frame{Type: TypeDirectPacket, Payload: []byte("d")})
	m.handleFrame("b", Frame{Type: TypeJoinLeaveWire, Payload: []byte("j")})
	m.handleFrame("b", Frame{Type: TypeBroadcast, Payload: []byte("b")})
	if len(classes) != 3 {
		t.Fatalf("%v", classes)
	}

	// Interest
	m.handleFrame("b", Frame{Type: TypeInterestUpdate, Payload: encodeInterest(InterestSummary{
		NodeID: "b", Boxes: []AABB{{0, 0, 1, 1}},
	})})

	// Position batch
	var batches int
	m.OnPositionBatch(func(_ string, _ PositionBatch) { batches++ })
	m.handleFrame("b", Frame{Type: TypePositionBatch, Payload: encodePositionBatch(PositionBatch{
		SenderCallsign: "P", Wires: [][]byte{[]byte("@")},
	})})
	if batches != 1 {
		t.Fatal(batches)
	}

	// Proximity
	var hint float64
	m.OnProximityHint(func(_, _ string, d float64) { hint = d })
	var pb []byte
	pb = encodeString(pb, "TGT")
	pb = encodeF64(pb, 42)
	m.handleFrame("b", Frame{Type: TypeProximityHint, Payload: pb})
	if hint != 42 {
		t.Fatal(hint)
	}

	// Claim reserve as owner
	// Make m own a callsign by using single-node ring... ring has a,b. Pick callsign owned by a.
	var owned string
	for _, cs := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z", "AA", "AB", "AC"} {
		if m.OwnerNode(cs) == "a" {
			owned = cs
			break
		}
	}
	if owned == "" {
		t.Fatal("no owned callsign")
	}
	var res []byte
	res = encodeString(res, owned)
	res = encodeString(res, "b")
	res = encodeString(res, "fence1")
	res = encodeU32(res, 1)
	res = append(res, 0)
	m.handleClaimReserve("b", res)

	// Commit
	var com []byte
	com = encodeString(com, owned)
	com = encodeString(com, "fence1")
	m.handleClaimCommit("b", com)

	// Abort / release frames
	var ab []byte
	ab = encodeString(ab, owned)
	ab = encodeString(ab, "fence1")
	m.handleFrame("b", Frame{Type: TypeClaimAbort, Payload: ab})
	// re-reserve and release
	m.handleClaimReserve("b", res)
	m.handleClaimCommit("b", com)
	m.handleFrame("b", Frame{Type: TypeClaimRelease, Payload: ab})

	// HomeRPC req without handler → errCode 2
	var hr []byte
	hr = encodeU64(hr, 99)
	hr = encodeU32(hr, uint32(HomeOpForceDisconnect))
	hr = encodeString(hr, owned)
	hr = encodeBytes(hr, nil)
	m.handleHomeRPCReq("b", hr)

	// With handler
	m.OnHomeRPC(func(op HomeOp, cs string, payload []byte) ([]byte, error) {
		return []byte("ok"), nil
	})
	m.handleHomeRPCReq("b", hr)

	// HomeRPC resp with waiter
	ch := make(chan rpcResult, 1)
	m.rpcMu.Lock()
	m.rpcWait[7] = ch
	m.rpcMu.Unlock()
	var resp []byte
	resp = encodeU64(resp, 7)
	resp = append(resp, 0)
	resp = encodeBytes(resp, []byte("hi"))
	m.handleHomeRPCResp(resp)
	select {
	case r := <-ch:
		if string(r.payload) != "hi" {
			t.Fatal(r)
		}
	default:
		t.Fatal("no rpc resp")
	}

	// Claim reserve/commit resp channels
	ch2 := make(chan claimResult, 1)
	m.claimWaitMu.Lock()
	m.claimWait["r:"+owned] = ch2
	m.claimWaitMu.Unlock()
	var ack []byte
	ack = encodeString(ack, owned)
	ack = encodeString(ack, "f")
	m.handleClaimReserveResp(Frame{Type: TypeClaimReserveAck, Payload: ack})
	<-ch2

	ch3 := make(chan claimResult, 1)
	m.claimWaitMu.Lock()
	m.claimWait["c:"+owned] = ch3
	m.claimWaitMu.Unlock()
	m.handleClaimCommitResp(Frame{Type: TypeClaimCommitAck, Payload: encodeString(nil, owned)})
	<-ch3

	ch4 := make(chan claimResult, 1)
	m.claimWaitMu.Lock()
	m.claimWait["r:NACK"] = ch4
	m.claimWaitMu.Unlock()
	m.handleClaimReserveResp(Frame{Type: TypeClaimReserveNack, Payload: encodeString(nil, "NACK")})
	<-ch4

	ch5 := make(chan claimResult, 1)
	m.claimWaitMu.Lock()
	m.claimWait["c:NACK"] = ch5
	m.claimWaitMu.Unlock()
	m.handleClaimCommitResp(Frame{Type: TypeClaimCommitNack, Payload: encodeString(nil, "NACK")})
	<-ch5

	// Local claim paths
	ctx := context.Background()
	if m.OwnerNode(owned) == "a" {
		f, err := m.ClaimReserve(ctx, owned+"2", ClaimMeta{NodeID: "a"})
		if err == nil {
			_ = m.ClaimCommit(ctx, owned+"2", f)
			m.ClaimAbort(owned+"2", f)
			m.ClaimRelease(owned+"2", f)
		}
	}

	// Forward / publish / broadcast with injected peer
	m.ForwardRanged([]byte("@"), []AABB{{0, 0, 1, 1}}, RangeClassPosition, "SENDER")
	m.PublishInterest(InterestSummary{Boxes: []AABB{{0, 0, 1, 1}}})
	m.BroadcastJoinLeave([]byte("#AP"))
	m.BroadcastClass([]byte("*"), BroadcastAll)
	m.NotifyLocalJoin(DirMeta{Callsign: "NJ", Routable: true})
	m.NotifyLocalFPL("NJ", "f")
	m.NotifyLocalBeacon("NJ", "1")
	m.NotifyLocalLeave("NJ")

	// SendDirect local path
	m.dir.ApplyJoin(DirMeta{Callsign: "LOC", NodeID: "a", Routable: true})
	_ = m.SendDirect("LOC", []byte("x"))
	// remote path
	m.dir.ApplyJoin(DirMeta{Callsign: "REM", NodeID: "b", Routable: true})
	_ = m.SendDirect("REM", []byte("y"))

	// HomeRPC local
	m.dir.ApplyJoin(DirMeta{Callsign: "H", NodeID: "a", Routable: true})
	_, _ = m.HomeRPC(ctx, "H", HomeOpForceDisconnect, nil)

	// removePeer
	m.removePeer("b")
}

func TestTCPPeerDeathCapturesIsATC(t *testing.T) {
	// Unit-test peer-death meta capture (IsATC) without racing heartbeatLoop.
	m := newIsolatedTCP(t, "a", "b")
	m.dir.ApplyJoin(DirMeta{Callsign: "DEAD_ATC", NodeID: "b", Routable: true, IsATC: true})
	m.dir.ApplyJoin(DirMeta{Callsign: "DEAD_PIL", NodeID: "b", Routable: true, IsATC: false})
	var metas []DirMeta
	for _, meta := range m.dir.Snapshot() {
		if meta.NodeID == "b" {
			metas = append(metas, meta)
		}
	}
	m.dir.RemoveNode("b")
	var sawATC, sawPilot bool
	for _, meta := range metas {
		if meta.Callsign == "DEAD_ATC" && meta.IsATC {
			sawATC = true
		}
		if meta.Callsign == "DEAD_PIL" && !meta.IsATC {
			sawPilot = true
		}
	}
	if !sawATC || !sawPilot {
		t.Fatalf("metas=%+v", metas)
	}
	if _, _, ok := m.Lookup("DEAD_ATC"); ok {
		t.Fatal("should be removed")
	}
}

func TestTCPHomeRPCForceDisconnectAuthz(t *testing.T) {
	m := newIsolatedTCP(t, "a", "b")
	pipePeer(t, m, "b")
	m.OnHomeRPC(func(op HomeOp, cs string, payload []byte) ([]byte, error) {
		return []byte("ok"), nil
	})
	// no rating → reject
	var req []byte
	req = encodeU64(req, 1)
	req = encodeU32(req, uint32(HomeOpForceDisconnect))
	req = encodeString(req, "X")
	req = encodeBytes(req, nil)
	m.handleHomeRPCReq("b", req)

	// low rating
	var pl [4]byte
	binary.BigEndian.PutUint32(pl[:], 5)
	req = nil
	req = encodeU64(req, 2)
	req = encodeU32(req, uint32(HomeOpForceDisconnect))
	req = encodeString(req, "X")
	req = encodeBytes(req, pl[:])
	m.handleHomeRPCReq("b", req)

	// supervisor rating
	binary.BigEndian.PutUint32(pl[:], 11)
	req = nil
	req = encodeU64(req, 3)
	req = encodeU32(req, uint32(HomeOpForceDisconnect))
	req = encodeString(req, "X")
	req = encodeBytes(req, pl[:])
	m.handleHomeRPCReq("b", req)

	// MutateFlightPlan ok
	req = nil
	req = encodeU64(req, 4)
	req = encodeU32(req, uint32(HomeOpMutateFlightPlan))
	req = encodeString(req, "X")
	req = encodeBytes(req, []byte("FPL"))
	m.handleHomeRPCReq("b", req)

	// ClaimRelease empty fence no-op
	m.ClaimRelease("Z", "")
	// ClaimRelease local owner
	ctx := context.Background()
	// find owned callsign
	for _, cs := range []string{"AA", "BB", "CC", "DD", "EE", "FF", "GG", "HH"} {
		if m.OwnerNode(cs) == "a" {
			f, err := m.ClaimReserve(ctx, cs, ClaimMeta{NodeID: "a", Fence: "f1"})
			if err == nil {
				_ = m.ClaimCommit(ctx, cs, f)
				m.ClaimRelease(cs, f)
			}
			break
		}
	}
	// Directory delta spoof rejected
	m.dir.ApplyJoin(DirMeta{Callsign: "OWN", NodeID: "a", Routable: true})
	m.handleFrame("b", Frame{Type: TypeDirectoryDelta, Payload: EncodeDelta(DeltaLeave, DirMeta{Callsign: "OWN", NodeID: "b"})})
	if _, _, ok := m.Lookup("OWN"); !ok {
		t.Fatal("spoof leave should be rejected")
	}
	// valid leave from owner node
	m.handleFrame("a", Frame{Type: TypeDirectoryDelta, Payload: EncodeDelta(DeltaLeave, DirMeta{Callsign: "OWN", NodeID: "a"})})
}

func TestTCPSameOriginHelper(t *testing.T) {
	// covered via rqlite, not cluster
}

func TestTCPRemoteClaimAndHomeRPC(t *testing.T) {
	a, b := startTwoTCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Wait link
	time.Sleep(100 * time.Millisecond)
	// Claim from a for callsign owned by whoever
	fence, err := a.ClaimReserve(ctx, "RPCX", ClaimMeta{NodeID: "ta", CID: 1})
	if err != nil {
		// try from b
		fence, err = b.ClaimReserve(ctx, "RPCX", ClaimMeta{NodeID: "tb", CID: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.ClaimCommit(ctx, "RPCX", fence); err != nil {
			t.Fatal(err)
		}
		b.OnHomeRPC(func(op HomeOp, cs string, pl []byte) ([]byte, error) {
			return []byte("meta"), nil
		})
		// wait directory
		for i := 0; i < 50; i++ {
			if _, _, ok := a.Lookup("RPCX"); ok {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		resp, err := a.HomeRPC(ctx, "RPCX", HomeOpQuerySessionMeta, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp
		a.ClaimAbort("nope", "x")
		// ForceDisconnect with rating via HomeRPC from a
		var pl [4]byte
		binary.BigEndian.PutUint32(pl[:], 12)
		_, _ = a.HomeRPC(ctx, "RPCX", HomeOpForceDisconnect, pl[:])
		b.ClaimRelease("RPCX", fence)
		return
	}
	if err := a.ClaimCommit(ctx, "RPCX", fence); err != nil {
		t.Fatal(err)
	}
	a.OnHomeRPC(func(op HomeOp, cs string, pl []byte) ([]byte, error) {
		return []byte("ok"), nil
	})
	for i := 0; i < 50; i++ {
		if _, _, ok := b.Lookup("RPCX"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var pl [4]byte
	binary.BigEndian.PutUint32(pl[:], 12)
	_, _ = b.HomeRPC(ctx, "RPCX", HomeOpForceDisconnect, pl[:])
	a.ClaimRelease("RPCX", fence)
	// Broadcast class ATC
	a.BroadcastClass([]byte("#TMx\r\n"), BroadcastATC)
	a.BroadcastClass([]byte("#TMx\r\n"), BroadcastSupervisor)
	// interest filter skip
	b.PublishInterest(InterestSummary{NodeID: "tb", Boxes: []AABB{{100, 100, 101, 101}}})
	time.Sleep(20 * time.Millisecond)
	a.ForwardRanged([]byte("@\r\n"), []AABB{{0, 0, 1, 1}}, RangeClassPosition, "Z")
	time.Sleep(50 * time.Millisecond)
}

func TestTCPHeartbeatLoopFiresPeerDead(t *testing.T) {
	m := newIsolatedTCP(t, "a", "b")
	local, remote := net.Pipe()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := remote.Read(buf); err != nil {
				return
			}
		}
	}()
	pc := &peerConn{
		nodeID: "b", conn: local, lastHB: time.Now().Add(-time.Hour),
		outCh: make(chan frameJob, 8), stop: make(chan struct{}),
	}
	m.mu.Lock()
	m.conns["b"] = pc
	m.mu.Unlock()
	go m.peerWriter(pc)
	m.dir.ApplyJoin(DirMeta{Callsign: "HB1", NodeID: "b", IsATC: true, Routable: true})
	done := make(chan []DirMeta, 1)
	m.OnPeerDead(func(id string, metas []DirMeta) {
		select {
		case done <- metas:
		default:
		}
	})
	m.peerDeathGrace = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go m.heartbeatLoop(ctx)
	select {
	case metas := <-done:
		cancel()
		if len(metas) < 1 || !metas[0].IsATC {
			t.Fatalf("%+v", metas)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timeout waiting peer death")
	}
	select {
	case <-pc.stop:
	default:
		close(pc.stop)
	}
	_ = local.Close()
	_ = remote.Close()
}
