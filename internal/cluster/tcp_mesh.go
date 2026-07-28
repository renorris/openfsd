package cluster

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// PeerAddr maps node_id → host:port.
type PeerAddr struct {
	NodeID string
	Addr   string // host:port
}

// TCPMeshConfig configures production TCP mesh.
type TCPMeshConfig struct {
	NodeID         string
	ListenAddr     string // host:port
	Peers          []PeerAddr
	ClaimTimeout   time.Duration
	PeerDeathGrace time.Duration
	// PSK is required when CLUSTER_ENABLED (reject empty).
	// Trust root: static CLUSTER_PEERS + PSK.
	PSK    string
	Logger *slog.Logger
}

const peerSendQueueSize = 256

// TCPMesh is the production length-prefixed TCP mesh.
type TCPMesh struct {
	cfg    TCPMeshConfig
	ring   *StablePeerRing
	claims *ClaimTable
	dir    *Directory

	// peerAddrs maps nodeID → configured host:port (Hello bind).
	peerAddrs map[string]string

	mu       sync.RWMutex
	interest map[string]InterestSummary
	conns    map[string]*peerConn // nodeID → conn

	ln net.Listener

	claimTimeout   time.Duration
	peerDeathGrace time.Duration
	logger         *slog.Logger

	onPeerDead      func(nodeID string, metas []DirMeta)
	onDirectWire    func(fromNode string, wire []byte, class WireClass)
	onHomeRPC       func(op HomeOp, callsign string, payload []byte) ([]byte, error)
	onPositionBatch func(fromNode string, batch PositionBatch)
	onProximityHint func(fromNode string, targetCallsign string, distanceM float64)

	// pending RPC
	rpcMu   sync.Mutex
	rpcSeq  uint64
	rpcWait map[uint64]chan rpcResult

	// claim RPC wait
	claimWaitMu sync.Mutex
	claimWait   map[string]chan claimResult // fence or callsign key

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type rpcResult struct {
	payload []byte
	errCode byte
}

type claimResult struct {
	fence string
	err   error
	meta  DirMeta
}

// peerConn: one writer goroutine drains outCh (KD-12 non-blocking enqueue).
type peerConn struct {
	nodeID string
	conn   net.Conn
	outCh  chan frameJob
	lastHB time.Time
	stop   chan struct{}
}

type frameJob struct {
	typ     byte
	payload []byte
	// optional result for reliable frames (nil = fire-and-forget / drop ok)
	done chan error
}

// NewTCPMesh constructs a TCP mesh (not started).
func NewTCPMesh(cfg TCPMeshConfig) (*TCPMesh, error) {
	if cfg.NodeID == "" || cfg.ListenAddr == "" {
		return nil, ErrInvalidConfig
	}
	if stringsTrimSpace(cfg.PSK) == "" {
		return nil, fmt.Errorf("%w: CLUSTER_MESH_PSK required when mesh enabled", ErrInvalidConfig)
	}
	ids := []string{cfg.NodeID}
	peerAddrs := map[string]string{cfg.NodeID: cfg.ListenAddr}
	for _, p := range cfg.Peers {
		if p.NodeID != "" && p.NodeID != cfg.NodeID {
			ids = append(ids, p.NodeID)
			peerAddrs[p.NodeID] = p.Addr
		}
	}
	ring, err := NewStablePeerRing(ids)
	if err != nil {
		return nil, err
	}
	ct := cfg.ClaimTimeout
	if ct <= 0 {
		ct = 400 * time.Millisecond
	}
	pg := cfg.PeerDeathGrace
	if pg <= 0 {
		pg = 15 * time.Second
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &TCPMesh{
		cfg:            cfg,
		ring:           ring,
		claims:         NewClaimTable(2 * time.Second),
		dir:            NewDirectory(),
		peerAddrs:      peerAddrs,
		interest:       make(map[string]InterestSummary),
		conns:          make(map[string]*peerConn),
		claimTimeout:   ct,
		peerDeathGrace: pg,
		logger:         log,
		rpcWait:        make(map[uint64]chan rpcResult),
		claimWait:      make(map[string]chan claimResult),
		stopCh:         make(chan struct{}),
	}, nil
}

func stringsTrimSpace(s string) string { return trimSpace(s) }

func (m *TCPMesh) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", m.cfg.ListenAddr)
	if err != nil {
		return err
	}
	m.ln = ln

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.acceptLoop(ctx)
	}()

	// Dial peers with node ID greater than self only (avoids double connections).
	for _, p := range m.cfg.Peers {
		if p.NodeID == m.cfg.NodeID || p.Addr == "" {
			continue
		}
		if p.NodeID <= m.cfg.NodeID {
			continue // peer with lower ID will dial us (or equal skipped)
		}
		peer := p
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.dialLoop(ctx, peer)
		}()
	}

	// Heartbeat / peer death
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.heartbeatLoop(ctx)
	}()
	return nil
}

func (m *TCPMesh) Stop() error {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		if m.ln != nil {
			_ = m.ln.Close()
		}
		m.mu.Lock()
		for _, pc := range m.conns {
			_ = pc.conn.Close()
		}
		m.mu.Unlock()
	})
	m.wg.Wait()
	return nil
}

func (m *TCPMesh) NodeID() string                { return m.cfg.NodeID }
func (m *TCPMesh) PeerIDs() []string             { return m.ring.IDs() }
func (m *TCPMesh) OwnerNode(cs string) string    { return m.ring.Owner(cs) }
func (m *TCPMesh) PeerDeathGrace() time.Duration { return m.peerDeathGrace }
func (m *TCPMesh) ClaimTimeout() time.Duration   { return m.claimTimeout }

func (m *TCPMesh) acceptLoop(ctx context.Context) {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			select {
			case <-m.stopCh:
				return
			default:
				return
			}
		}
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.serveConn(conn, true)
		}()
	}
}

func (m *TCPMesh) dialLoop(ctx context.Context, peer PeerAddr) {
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", peer.Addr)
		if err != nil {
			time.Sleep(backoff)
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
		m.serveConn(conn, false)
		// reconnect after disconnect
		select {
		case <-m.stopCh:
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (m *TCPMesh) serveConn(conn net.Conn, inbound bool) {
	defer conn.Close()
	// Hello exchange
	hello := encodeString(nil, m.cfg.NodeID)
	hello = encodeString(hello, m.cfg.PSK)
	if err := EncodeFrame(conn, TypeHello, hello); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	fr, err := DecodeFrame(conn)
	if err != nil || fr.Type != TypeHello {
		return
	}
	peerID, rest, err := decodeString(fr.Payload)
	if err != nil || peerID == "" {
		return
	}
	psk, _, _ := decodeString(rest)
	if psk != m.cfg.PSK {
		m.logger.Warn("mesh hello PSK mismatch", "peer", peerID)
		return
	}
	// Allowlist: peer must be in static CLUSTER_PEERS ring
	if _, ok := m.peerAddrs[peerID]; !ok && peerID != m.cfg.NodeID {
		m.logger.Warn("mesh reject unknown peer (not in CLUSTER_PEERS)", "peer", peerID)
		return
	}
	allowed := false
	for _, id := range m.ring.IDs() {
		if id == peerID {
			allowed = true
			break
		}
	}
	if !allowed || peerID == m.cfg.NodeID {
		m.logger.Warn("mesh reject unknown peer", "peer", peerID)
		return
	}
	// R2-10: log RemoteAddr vs configured PeerAddr (NAT may differ; do not hard-fail).
	if expected, ok := m.peerAddrs[peerID]; ok && expected != "" {
		if ra := conn.RemoteAddr(); ra != nil {
			host, _, _ := net.SplitHostPort(ra.String())
			expHost, _, _ := net.SplitHostPort(expected)
			if expHost == "" {
				expHost = expected
			}
			if host != "" && expHost != "" && host != expHost && expHost != "0.0.0.0" {
				m.logger.Warn("mesh Hello RemoteAddr host differs from CLUSTER_PEERS",
					"peer", peerID, "remote", host, "configured", expHost)
			}
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	pc := &peerConn{
		nodeID: peerID,
		conn:   conn,
		outCh:  make(chan frameJob, peerSendQueueSize),
		lastHB: time.Now(),
		stop:   make(chan struct{}),
	}
	m.mu.Lock()
	// Do not replace an authenticated live peer without closing the old one cleanly.
	if old, ok := m.conns[peerID]; ok {
		close(old.stop)
		_ = old.conn.Close()
	}
	m.conns[peerID] = pc
	m.mu.Unlock()

	// Writer goroutine (KD-12)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.peerWriter(pc)
	}()

	// Send directory snapshot (enqueue)
	snap := EncodeSnapshot(m.dir.Snapshot())
	_ = m.enqueue(pc, TypeDirectorySnapshot, snap, false)

	// Read loop
	for {
		select {
		case <-m.stopCh:
			close(pc.stop)
			return
		case <-pc.stop:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(m.peerDeathGrace + 5*time.Second))
		fr, err := DecodeFrame(conn)
		if err != nil {
			m.removePeer(peerID)
			return
		}
		pc.lastHB = time.Now()
		m.handleFrame(peerID, fr)
	}
}

func (m *TCPMesh) peerWriter(pc *peerConn) {
	for {
		select {
		case <-m.stopCh:
			return
		case <-pc.stop:
			return
		case job, ok := <-pc.outCh:
			if !ok {
				return
			}
			err := EncodeFrame(pc.conn, job.typ, job.payload)
			if job.done != nil {
				select {
				case job.done <- err:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}
}

// enqueue enqueues a frame for the peer writer.
// reliable=true: wait for write completion (claim/HomeRPC only; may block caller).
// reliable=false: non-blocking fire-and-forget; drop-oldest on full (KD-12 safe for gnet).
func (m *TCPMesh) enqueue(pc *peerConn, typ byte, payload []byte, reliable bool) error {
	if pc == nil {
		return ErrPeerDown
	}
	job := frameJob{typ: typ, payload: payload}
	if reliable {
		job.done = make(chan error, 1)
	}
	select {
	case pc.outCh <- job:
		if !reliable {
			return nil
		}
		// Wait for writer only for claim/HomeRPC (already off gnet via authPool).
		select {
		case err := <-job.done:
			return err
		case <-time.After(m.claimTimeout):
			return ErrClaimTimeout
		case <-m.stopCh:
			return ErrMeshNotStarted
		}
	default:
		if reliable {
			return ErrPeerDown // queue full → fail RPC (no silent success)
		}
		// drop-oldest for positions/text (never block gnet)
		select {
		case <-pc.outCh:
		default:
		}
		select {
		case pc.outCh <- job:
		default:
		}
		return nil
	}
}

func (m *TCPMesh) removePeer(peerID string) {
	m.mu.Lock()
	if pc, ok := m.conns[peerID]; ok {
		select {
		case <-pc.stop:
		default:
			close(pc.stop)
		}
		_ = pc.conn.Close()
		delete(m.conns, peerID)
	}
	m.mu.Unlock()
}

func (m *TCPMesh) sendTo(nodeID string, typ byte, payload []byte) error {
	m.mu.RLock()
	pc := m.conns[nodeID]
	m.mu.RUnlock()
	if pc == nil {
		return ErrPeerDown
	}
	// Reliable path for claim/HomeRPC
	reliable := typ == TypeClaimReserve || typ == TypeClaimCommit ||
		typ == TypeClaimAbort || typ == TypeClaimRelease ||
		typ == TypeHomeRPCReq || typ == TypeHomeRPCResp ||
		typ == TypeClaimReserveAck || typ == TypeClaimReserveNack ||
		typ == TypeClaimCommitAck || typ == TypeClaimCommitNack
	return m.enqueue(pc, typ, payload, reliable)
}

func (m *TCPMesh) broadcast(typ byte, payload []byte) {
	m.mu.RLock()
	conns := make([]*peerConn, 0, len(m.conns))
	for _, pc := range m.conns {
		conns = append(conns, pc)
	}
	m.mu.RUnlock()
	for _, pc := range conns {
		_ = m.enqueue(pc, typ, payload, false)
	}
}

func (m *TCPMesh) handleFrame(from string, fr Frame) {
	switch fr.Type {
	case TypeHeartbeat:
		// update lastHB already
	case TypeDirectorySnapshot:
		entries, err := DecodeSnapshot(fr.Payload)
		if err == nil {
			// Only accept rows hosted on the peer that sent the snapshot (R2-2).
			m.dir.ApplySnapshotFrom(from, entries)
		}
	case TypeDirectoryDelta:
		kind, meta, err := DecodeDelta(fr.Payload)
		if err != nil {
			return
		}
		// Ownership: only accept leave/meta for callsigns hosted on `from`.
		switch kind {
		case DeltaJoin:
			if meta.NodeID == "" {
				meta.NodeID = from
			}
			if meta.NodeID != from {
				return // spoofed host node
			}
			m.dir.ApplyJoin(meta)
		case DeltaLeave:
			if node, _, ok := m.dir.Lookup(meta.Callsign); ok && node != from {
				return
			}
			m.dir.ApplyLeave(meta.Callsign)
		case DeltaMeta:
			if node, _, ok := m.dir.Lookup(meta.Callsign); ok && node != from {
				return
			}
			meta.NodeID = from
			m.dir.ApplyJoin(meta)
		}
	case TypeClaimReserve:
		m.handleClaimReserve(from, fr.Payload)
	case TypeClaimReserveAck, TypeClaimReserveNack:
		m.handleClaimReserveResp(fr)
	case TypeClaimCommit:
		m.handleClaimCommit(from, fr.Payload)
	case TypeClaimCommitAck, TypeClaimCommitNack:
		m.handleClaimCommitResp(fr)
	case TypeClaimAbort:
		cs, rest, err := decodeString(fr.Payload)
		if err != nil {
			return
		}
		fence, _, _ := decodeString(rest)
		m.claims.Abort(cs, fence)
	case TypeClaimRelease:
		cs, rest, err := decodeString(fr.Payload)
		if err != nil {
			return
		}
		fence, _, _ := decodeString(rest)
		// R2-1: empty fence never force-frees on wire.
		if fence == "" {
			return
		}
		if m.claims.Release(cs, fence) {
			m.dir.ApplyLeave(cs)
			m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaLeave, DirMeta{Callsign: cs, NodeID: m.cfg.NodeID}))
		}
	case TypeDirectPacket:
		if m.onDirectWire != nil {
			m.onDirectWire(from, fr.Payload, WireDirect)
		}
	case TypeJoinLeaveWire:
		if m.onDirectWire != nil {
			m.onDirectWire(from, fr.Payload, WireJoinLeave)
		}
	case TypeBroadcast:
		if m.onDirectWire != nil {
			m.onDirectWire(from, fr.Payload, WireBroadcast)
		}
	case TypePositionBatch:
		if m.onPositionBatch != nil {
			batch, err := decodePositionBatch(fr.Payload)
			if err == nil {
				m.onPositionBatch(from, batch)
			}
		}
	case TypeTextRanged:
		if m.onDirectWire != nil {
			// Pass full payload (boxes prefix + wire); receiver parses for re-fan filter.
			m.onDirectWire(from, fr.Payload, WireRanged)
		}
	case TypeInterestUpdate:
		sum, err := decodeInterest(fr.Payload)
		if err == nil {
			m.mu.Lock()
			m.interest[sum.NodeID] = sum
			m.mu.Unlock()
		}
	case TypeProximityHint:
		if m.onProximityHint != nil {
			cs, rest, err := decodeString(fr.Payload)
			if err != nil {
				return
			}
			d, _, err := decodeF64(rest)
			if err != nil {
				return
			}
			m.onProximityHint(from, cs, d)
		}
	case TypeHomeRPCReq:
		m.handleHomeRPCReq(from, fr.Payload)
	case TypeHomeRPCResp:
		m.handleHomeRPCResp(fr.Payload)
	}
}

func (m *TCPMesh) handleClaimReserve(from string, payload []byte) {
	cs, rest, err := decodeString(payload)
	if err != nil {
		return
	}
	nodeID, rest, err := decodeString(rest)
	if err != nil {
		return
	}
	fence, rest, err := decodeString(rest)
	if err != nil {
		return
	}
	cid, rest, err := decodeU32(rest)
	if err != nil {
		return
	}
	isATC := false
	if len(rest) > 0 {
		isATC = rest[0] != 0
	}
	// Only process if we are owner
	if m.ring.Owner(cs) != m.cfg.NodeID {
		_ = m.sendTo(from, TypeClaimReserveNack, encodeString(nil, cs))
		return
	}
	f, err := m.claims.Reserve(cs, ClaimMeta{
		NodeID: nodeID,
		Fence:  fence,
		CID:    int(cid),
		IsATC:  isATC,
	})
	if err != nil {
		_ = m.sendTo(from, TypeClaimReserveNack, encodeString(nil, cs))
		return
	}
	var b []byte
	b = encodeString(b, cs)
	b = encodeString(b, f)
	_ = m.sendTo(from, TypeClaimReserveAck, b)
}

func (m *TCPMesh) handleClaimReserveResp(fr Frame) {
	cs, rest, err := decodeString(fr.Payload)
	if err != nil {
		return
	}
	m.claimWaitMu.Lock()
	ch := m.claimWait["r:"+cs]
	m.claimWaitMu.Unlock()
	if ch == nil {
		return
	}
	if fr.Type == TypeClaimReserveAck {
		fence, _, _ := decodeString(rest)
		select {
		case ch <- claimResult{fence: fence}:
		default:
		}
	} else {
		select {
		case ch <- claimResult{err: ErrClaimInUse}:
		default:
		}
	}
}

func (m *TCPMesh) handleClaimCommit(from string, payload []byte) {
	cs, rest, err := decodeString(payload)
	if err != nil {
		return
	}
	fence, _, err := decodeString(rest)
	if err != nil {
		return
	}
	if m.ring.Owner(cs) != m.cfg.NodeID {
		_ = m.sendTo(from, TypeClaimCommitNack, encodeString(nil, cs))
		return
	}
	meta, err := m.claims.Commit(cs, fence)
	if err != nil {
		_ = m.sendTo(from, TypeClaimCommitNack, encodeString(nil, cs))
		return
	}
	m.dir.ApplyJoin(meta)
	m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaJoin, meta))
	_ = m.sendTo(from, TypeClaimCommitAck, encodeString(nil, cs))
}

func (m *TCPMesh) handleClaimCommitResp(fr Frame) {
	cs, _, err := decodeString(fr.Payload)
	if err != nil {
		return
	}
	m.claimWaitMu.Lock()
	ch := m.claimWait["c:"+cs]
	m.claimWaitMu.Unlock()
	if ch == nil {
		return
	}
	if fr.Type == TypeClaimCommitAck {
		select {
		case ch <- claimResult{}:
		default:
		}
	} else {
		select {
		case ch <- claimResult{err: ErrClaimFence}:
		default:
		}
	}
}

func (m *TCPMesh) handleHomeRPCReq(from string, payload []byte) {
	seq, rest, err := decodeU64(payload)
	if err != nil {
		return
	}
	opB, rest, err := decodeU32(rest)
	if err != nil {
		return
	}
	cs, rest, err := decodeString(rest)
	if err != nil {
		return
	}
	pl, _, err := decodeBytes(rest)
	if err != nil {
		return
	}
	// ForceDisconnect: require originator rating in payload (u32 BE) ≥ supervisor (11).
	if HomeOp(opB) == HomeOpForceDisconnect {
		if len(pl) < 4 {
			var b []byte
			b = encodeU64(b, seq)
			b = append(b, 1) // conflict / unauthorized
			b = encodeBytes(b, nil)
			_ = m.sendTo(from, TypeHomeRPCResp, b)
			return
		}
		rating := binary.BigEndian.Uint32(pl[:4])
		const networkRatingSupervisor = 11
		if rating < networkRatingSupervisor {
			var b []byte
			b = encodeU64(b, seq)
			b = append(b, 1)
			b = encodeBytes(b, nil)
			_ = m.sendTo(from, TypeHomeRPCResp, b)
			return
		}
		pl = pl[4:] // remainder unused
	}
	var resp []byte
	var errCode byte
	if m.onHomeRPC != nil {
		r, e := m.onHomeRPC(HomeOp(opB), cs, pl)
		resp = r
		if e != nil {
			errCode = 1
			if e == ErrHomeRPCNotFound {
				errCode = 2
			}
		}
	} else {
		errCode = 2
	}
	var b []byte
	b = encodeU64(b, seq)
	b = append(b, errCode)
	b = encodeBytes(b, resp)
	_ = m.sendTo(from, TypeHomeRPCResp, b)
}

func (m *TCPMesh) handleHomeRPCResp(payload []byte) {
	seq, rest, err := decodeU64(payload)
	if err != nil {
		return
	}
	if len(rest) < 1 {
		return
	}
	errCode := rest[0]
	pl, _, _ := decodeBytes(rest[1:])
	m.rpcMu.Lock()
	ch := m.rpcWait[seq]
	delete(m.rpcWait, seq)
	m.rpcMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- rpcResult{payload: pl, errCode: errCode}:
	default:
	}
}

func (m *TCPMesh) heartbeatLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	lastSeen := map[string]time.Time{}
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			m.broadcast(TypeHeartbeat, nil)
			now := time.Now()
			m.mu.RLock()
			for id, pc := range m.conns {
				lastSeen[id] = pc.lastHB
			}
			m.mu.RUnlock()
			for id, ts := range lastSeen {
				if now.Sub(ts) > m.peerDeathGrace {
					// Capture DirMeta (IsATC) before remove.
					var metas []DirMeta
					for _, meta := range m.dir.Snapshot() {
						if meta.NodeID == id {
							metas = append(metas, meta)
						}
					}
					m.dir.RemoveNode(id)
					m.claims.ReleaseNode(id)
					m.mu.Lock()
					if pc, ok := m.conns[id]; ok {
						select {
						case <-pc.stop:
						default:
							close(pc.stop)
						}
						_ = pc.conn.Close()
						delete(m.conns, id)
					}
					m.mu.Unlock()
					delete(lastSeen, id)
					if m.onPeerDead != nil {
						m.onPeerDead(id, metas)
					}
				}
			}
		}
	}
}

func (m *TCPMesh) ClaimReserve(ctx context.Context, callsign string, meta ClaimMeta) (string, error) {
	if meta.NodeID == "" {
		meta.NodeID = m.cfg.NodeID
	}
	owner := m.ring.Owner(callsign)
	if owner == m.cfg.NodeID {
		return m.claims.Reserve(callsign, meta)
	}
	fence := meta.Fence
	if fence == "" {
		fence = uuid.NewString()
	}
	var b []byte
	b = encodeString(b, callsign)
	b = encodeString(b, meta.NodeID)
	b = encodeString(b, fence)
	b = encodeU32(b, uint32(meta.CID))
	if meta.IsATC {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	ch := make(chan claimResult, 1)
	m.claimWaitMu.Lock()
	m.claimWait["r:"+callsign] = ch
	m.claimWaitMu.Unlock()
	defer func() {
		m.claimWaitMu.Lock()
		delete(m.claimWait, "r:"+callsign)
		m.claimWaitMu.Unlock()
	}()
	if err := m.sendTo(owner, TypeClaimReserve, b); err != nil {
		return "", err
	}
	timeout := m.claimTimeout
	select {
	case <-ctx.Done():
		return "", ErrClaimTimeout
	case <-time.After(timeout):
		return "", ErrClaimTimeout
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		if r.fence != "" {
			return r.fence, nil
		}
		return fence, nil
	}
}

func (m *TCPMesh) ClaimCommit(ctx context.Context, callsign, fence string) error {
	owner := m.ring.Owner(callsign)
	if owner == m.cfg.NodeID {
		meta, err := m.claims.Commit(callsign, fence)
		if err != nil {
			return err
		}
		m.dir.ApplyJoin(meta)
		m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaJoin, meta))
		return nil
	}
	var b []byte
	b = encodeString(b, callsign)
	b = encodeString(b, fence)
	ch := make(chan claimResult, 1)
	m.claimWaitMu.Lock()
	m.claimWait["c:"+callsign] = ch
	m.claimWaitMu.Unlock()
	defer func() {
		m.claimWaitMu.Lock()
		delete(m.claimWait, "c:"+callsign)
		m.claimWaitMu.Unlock()
	}()
	if err := m.sendTo(owner, TypeClaimCommit, b); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ErrClaimTimeout
	case <-time.After(m.claimTimeout):
		return ErrClaimTimeout
	case r := <-ch:
		return r.err
	}
}

func (m *TCPMesh) ClaimAbort(callsign, fence string) {
	owner := m.ring.Owner(callsign)
	if owner == m.cfg.NodeID {
		m.claims.Abort(callsign, fence)
		return
	}
	var b []byte
	b = encodeString(b, callsign)
	b = encodeString(b, fence)
	_ = m.sendTo(owner, TypeClaimAbort, b)
}

func (m *TCPMesh) ClaimRelease(callsign, fence string) {
	// Fence required (no empty force from remote peers).
	if fence == "" {
		return
	}
	owner := m.ring.Owner(callsign)
	if owner == m.cfg.NodeID {
		if m.claims.Release(callsign, fence) {
			m.dir.ApplyLeave(callsign)
			m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaLeave, DirMeta{Callsign: callsign, NodeID: m.cfg.NodeID}))
		}
		return
	}
	var b []byte
	b = encodeString(b, callsign)
	b = encodeString(b, fence)
	_ = m.sendTo(owner, TypeClaimRelease, b)
	m.dir.ApplyLeave(callsign)
}

func (m *TCPMesh) Lookup(callsign string) (string, DirMeta, bool) {
	return m.dir.Lookup(callsign)
}

func (m *TCPMesh) SendDirect(callsign string, wire []byte) error {
	nodeID, _, ok := m.dir.Lookup(callsign)
	if !ok {
		return ErrDirectNotFound
	}
	if nodeID == m.cfg.NodeID {
		if m.onDirectWire != nil {
			m.onDirectWire(m.cfg.NodeID, wire, WireDirect)
		}
		return nil
	}
	return m.sendTo(nodeID, TypeDirectPacket, wire)
}

func (m *TCPMesh) HomeRPC(ctx context.Context, callsign string, op HomeOp, payload []byte) ([]byte, error) {
	nodeID, _, ok := m.dir.Lookup(callsign)
	if !ok {
		return nil, ErrHomeRPCNotFound
	}
	if nodeID == m.cfg.NodeID {
		if m.onHomeRPC == nil {
			return nil, ErrHomeRPCNotFound
		}
		return m.onHomeRPC(op, callsign, payload)
	}
	m.rpcMu.Lock()
	m.rpcSeq++
	seq := m.rpcSeq
	ch := make(chan rpcResult, 1)
	m.rpcWait[seq] = ch
	m.rpcMu.Unlock()
	defer func() {
		m.rpcMu.Lock()
		delete(m.rpcWait, seq)
		m.rpcMu.Unlock()
	}()
	var b []byte
	b = encodeU64(b, seq)
	b = encodeU32(b, uint32(op))
	b = encodeString(b, callsign)
	b = encodeBytes(b, payload)
	if err := m.sendTo(nodeID, TypeHomeRPCReq, b); err != nil {
		return nil, err
	}
	timeout := m.claimTimeout
	if timeout < time.Second {
		timeout = time.Second
	}
	select {
	case <-ctx.Done():
		return nil, ErrHomeRPCTimeout
	case <-time.After(timeout):
		return nil, ErrHomeRPCTimeout
	case r := <-ch:
		if r.errCode == 2 {
			return nil, ErrHomeRPCNotFound
		}
		if r.errCode != 0 {
			return nil, ErrHomeRPCConflict
		}
		return r.payload, nil
	}
}

func (m *TCPMesh) ForwardTextRanged(wire []byte, senderBoxes []AABB) {
	// Non-blocking enqueue (R3-2): never wait on TCP write from caller/gnet.
	// Payload: u32 nBoxes | boxes... | wire bytes
	wire = SanitizeWireBytes(wire)
	var payload []byte
	payload = encodeU32(payload, uint32(len(senderBoxes)))
	for _, a := range senderBoxes {
		payload = encodeF64(payload, a.MinLat)
		payload = encodeF64(payload, a.MinLon)
		payload = encodeF64(payload, a.MaxLat)
		payload = encodeF64(payload, a.MaxLon)
	}
	payload = append(payload, wire...)

	m.mu.RLock()
	interests := make(map[string]InterestSummary, len(m.interest))
	for k, v := range m.interest {
		interests[k] = v
	}
	conns := make(map[string]*peerConn, len(m.conns))
	for k, v := range m.conns {
		conns[k] = v
	}
	m.mu.RUnlock()
	for id, pc := range conns {
		sum := interests[id]
		if len(sum.Boxes) > 0 && !AnyAABBOverlap(senderBoxes, sum.Boxes) {
			continue
		}
		_ = m.enqueue(pc, TypeTextRanged, payload, false)
	}
}

func (m *TCPMesh) ForwardRanged(wire []byte, senderBoxes []AABB, class RangeClass, senderCallsign string) {
	wire = SanitizeWireBytes(wire)
	senderCallsign = SanitizeMeshString(senderCallsign, 32)
	batch := PositionBatch{
		SenderCallsign: senderCallsign,
		SenderBoxes:    senderBoxes,
		Velocity:       class == RangeClassVelocity,
		Wires:          [][]byte{append([]byte(nil), wire...)},
	}
	payload := encodePositionBatch(batch)

	m.mu.RLock()
	interests := make(map[string]InterestSummary, len(m.interest))
	for k, v := range m.interest {
		interests[k] = v
	}
	conns := make(map[string]*peerConn, len(m.conns))
	for k, v := range m.conns {
		conns[k] = v
	}
	m.mu.RUnlock()

	for id, pc := range conns {
		sum := interests[id]
		if len(sum.Boxes) > 0 && !AnyAABBOverlap(senderBoxes, sum.Boxes) {
			continue
		}
		// Non-blocking enqueue; optional coalesce via drop-oldest on full queue.
		_ = m.enqueue(pc, TypePositionBatch, payload, false)
	}
}

func (m *TCPMesh) PublishInterest(sum InterestSummary) {
	if sum.NodeID == "" {
		sum.NodeID = m.cfg.NodeID
	}
	m.mu.Lock()
	m.interest[sum.NodeID] = sum
	m.mu.Unlock()
	m.broadcast(TypeInterestUpdate, encodeInterest(sum))
}

func (m *TCPMesh) BroadcastJoinLeave(wire []byte) {
	m.broadcast(TypeJoinLeaveWire, SanitizeWireBytes(wire))
}

func (m *TCPMesh) BroadcastClass(wire []byte, class BroadcastClass) {
	payload := append([]byte{byte(class)}, SanitizeWireBytes(wire)...)
	m.broadcast(TypeBroadcast, payload)
}

func (m *TCPMesh) SendProximityHint(homeNode, targetCallsign string, distanceM float64) {
	if homeNode == "" || homeNode == m.cfg.NodeID {
		if m.onProximityHint != nil {
			m.onProximityHint(m.cfg.NodeID, targetCallsign, distanceM)
		}
		return
	}
	var b []byte
	b = encodeString(b, SanitizeMeshString(targetCallsign, 32))
	b = encodeF64(b, distanceM)
	_ = m.sendTo(homeNode, TypeProximityHint, b)
}

func (m *TCPMesh) NotifyLocalJoin(meta DirMeta) {
	if meta.NodeID == "" {
		meta.NodeID = m.cfg.NodeID
	}
	meta.Routable = true
	meta.Fence = "" // never broadcast
	m.dir.ApplyJoin(meta)
	m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaJoin, meta))
}

func (m *TCPMesh) NotifyLocalLeave(callsign string) {
	m.dir.ApplyLeave(callsign)
	m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaLeave, DirMeta{Callsign: callsign, NodeID: m.cfg.NodeID}))
}

func (m *TCPMesh) NotifyLocalFPL(callsign, fplInfo string) {
	fplInfo = SanitizeMeshString(fplInfo, 2048)
	m.dir.ApplyMeta(callsign, &fplInfo, nil)
	_, meta, ok := m.dir.Lookup(callsign)
	if ok {
		meta.FPLInfo = fplInfo
		meta.Fence = ""
		m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaMeta, meta))
	}
}

func (m *TCPMesh) NotifyLocalBeacon(callsign, beacon string) {
	beacon = SanitizeMeshString(beacon, 8)
	m.dir.ApplyMeta(callsign, nil, &beacon)
	_, meta, ok := m.dir.Lookup(callsign)
	if ok {
		meta.AssignedBeacon = beacon
		meta.Fence = ""
		m.broadcast(TypeDirectoryDelta, EncodeDelta(DeltaMeta, meta))
	}
}

func (m *TCPMesh) OnPeerDead(fn func(nodeID string, metas []DirMeta)) {
	m.onPeerDead = fn
}
func (m *TCPMesh) OnDirectWire(fn func(fromNode string, wire []byte, class WireClass)) {
	m.onDirectWire = fn
}
func (m *TCPMesh) OnHomeRPC(fn func(op HomeOp, callsign string, payload []byte) ([]byte, error)) {
	m.onHomeRPC = fn
}
func (m *TCPMesh) OnPositionBatch(fn func(fromNode string, batch PositionBatch)) {
	m.onPositionBatch = fn
}
func (m *TCPMesh) OnProximityHint(fn func(fromNode string, targetCallsign string, distanceM float64)) {
	m.onProximityHint = fn
}

var _ Mesh = (*TCPMesh)(nil)

func encodePositionBatch(b PositionBatch) []byte {
	var out []byte
	out = encodeString(out, b.SenderCallsign)
	out = encodeU32(out, uint32(len(b.SenderBoxes)))
	for _, a := range b.SenderBoxes {
		out = encodeF64(out, a.MinLat)
		out = encodeF64(out, a.MinLon)
		out = encodeF64(out, a.MaxLat)
		out = encodeF64(out, a.MaxLon)
	}
	if b.Velocity {
		out = append(out, 1)
	} else {
		out = append(out, 0)
	}
	out = encodeU32(out, uint32(len(b.Wires)))
	for _, w := range b.Wires {
		out = encodeBytes(out, w)
	}
	out = encodeF64(out, b.ClosestDistanceM)
	return out
}

func decodePositionBatch(p []byte) (PositionBatch, error) {
	var b PositionBatch
	var err error
	b.SenderCallsign, p, err = decodeString(p)
	if err != nil {
		return b, err
	}
	n, p, err := decodeU32(p)
	if err != nil {
		return b, err
	}
	for i := uint32(0); i < n; i++ {
		var a AABB
		a.MinLat, p, err = decodeF64(p)
		if err != nil {
			return b, err
		}
		a.MinLon, p, err = decodeF64(p)
		if err != nil {
			return b, err
		}
		a.MaxLat, p, err = decodeF64(p)
		if err != nil {
			return b, err
		}
		a.MaxLon, p, err = decodeF64(p)
		if err != nil {
			return b, err
		}
		b.SenderBoxes = append(b.SenderBoxes, a)
	}
	if len(p) < 1 {
		return b, ErrShortFrame
	}
	b.Velocity = p[0] != 0
	p = p[1:]
	nw, p, err := decodeU32(p)
	if err != nil {
		return b, err
	}
	for i := uint32(0); i < nw; i++ {
		var w []byte
		w, p, err = decodeBytes(p)
		if err != nil {
			return b, err
		}
		b.Wires = append(b.Wires, w)
	}
	b.ClosestDistanceM, p, err = decodeF64(p)
	return b, err
}

func encodeInterest(sum InterestSummary) []byte {
	var b []byte
	b = encodeString(b, sum.NodeID)
	b = encodeU32(b, uint32(len(sum.Boxes)))
	for _, a := range sum.Boxes {
		b = encodeF64(b, a.MinLat)
		b = encodeF64(b, a.MinLon)
		b = encodeF64(b, a.MaxLat)
		b = encodeF64(b, a.MaxLon)
	}
	return b
}

func decodeInterest(p []byte) (InterestSummary, error) {
	var sum InterestSummary
	var err error
	sum.NodeID, p, err = decodeString(p)
	if err != nil {
		return sum, err
	}
	n, p, err := decodeU32(p)
	if err != nil {
		return sum, err
	}
	for i := uint32(0); i < n; i++ {
		var a AABB
		a.MinLat, p, err = decodeF64(p)
		if err != nil {
			return sum, err
		}
		a.MinLon, p, err = decodeF64(p)
		if err != nil {
			return sum, err
		}
		a.MaxLat, p, err = decodeF64(p)
		if err != nil {
			return sum, err
		}
		a.MaxLon, p, err = decodeF64(p)
		if err != nil {
			return sum, err
		}
		sum.Boxes = append(sum.Boxes, a)
	}
	return sum, nil
}

// ParseClusterPeers parses "id=host:port,id2=host2:port" into PeerAddr list.
func ParseClusterPeers(s string) ([]PeerAddr, error) {
	s = trimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []PeerAddr
	for _, part := range splitComma(s) {
		part = trimSpace(part)
		if part == "" {
			continue
		}
		// id=host:port — split on first =
		i := indexByte(part, '=')
		if i <= 0 {
			return nil, fmt.Errorf("cluster peer %q: want id=host:port", part)
		}
		id := trimSpace(part[:i])
		addr := trimSpace(part[i+1:])
		if id == "" || addr == "" {
			return nil, fmt.Errorf("cluster peer %q: empty id or addr", part)
		}
		out = append(out, PeerAddr{NodeID: id, Addr: addr})
	}
	if len(out) > 8 {
		return nil, fmt.Errorf("cluster: max 8 peers, got %d", len(out))
	}
	return out, nil
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// Ensure binary import used (header helpers).
var _ = binary.BigEndian

// silence unused io in some builds
var _ = io.EOF
