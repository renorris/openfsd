package cluster

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryMesh is an in-process multi-node mesh for tests and single-process sim.
// Multiple MemoryMesh instances share a MemoryHub; each has its own node ID,
// claim table (when owner), directory, and interest view.
type MemoryHub struct {
	mu    sync.RWMutex
	nodes map[string]*MemoryMesh
	// Optional one-way delay for high-latency simulation.
	delay time.Duration
}

// NewMemoryHub creates a shared hub.
func NewMemoryHub() *MemoryHub {
	return &MemoryHub{nodes: make(map[string]*MemoryMesh)}
}

// SetDelay injects artificial one-way latency between nodes (tests).
func (h *MemoryHub) SetDelay(d time.Duration) {
	h.mu.Lock()
	h.delay = d
	h.mu.Unlock()
}

func (h *MemoryHub) delayCopy() time.Duration {
	h.mu.RLock()
	d := h.delay
	h.mu.RUnlock()
	return d
}

// MemoryMeshConfig configures one node.
type MemoryMeshConfig struct {
	NodeID         string
	PeerIDs        []string // full ring including self
	ClaimTimeout   time.Duration
	PeerDeathGrace time.Duration
	Logger         *slog.Logger
}

// MemoryMesh implements Mesh in-process.
type MemoryMesh struct {
	cfg    MemoryMeshConfig
	hub    *MemoryHub
	ring   *StablePeerRing
	claims *ClaimTable
	dir    *Directory

	mu       sync.RWMutex
	started  bool
	interest map[string]InterestSummary // peer → last interest
	// remote hints for $SF are server-side; mesh only delivers ProximityHint

	claimTimeout   time.Duration
	peerDeathGrace time.Duration

	onPeerDead      func(nodeID string, metas []DirMeta)
	onDirectWire    func(fromNode string, wire []byte, class WireClass)
	onHomeRPC       func(op HomeOp, callsign string, payload []byte) ([]byte, error)
	onPositionBatch func(fromNode string, batch PositionBatch)
	onProximityHint func(fromNode string, targetCallsign string, distanceM float64)

	// peer liveness for death simulation
	alive    map[string]bool
	deadMu   sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewMemoryMesh registers a node on hub.
func NewMemoryMesh(hub *MemoryHub, cfg MemoryMeshConfig) (*MemoryMesh, error) {
	if cfg.NodeID == "" {
		return nil, ErrInvalidConfig
	}
	peers := cfg.PeerIDs
	if len(peers) == 0 {
		peers = []string{cfg.NodeID}
	}
	ring, err := NewStablePeerRing(peers)
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
	m := &MemoryMesh{
		cfg:            cfg,
		hub:            hub,
		ring:           ring,
		claims:         NewClaimTable(2 * time.Second),
		dir:            NewDirectory(),
		interest:       make(map[string]InterestSummary),
		claimTimeout:   ct,
		peerDeathGrace: pg,
		alive:          make(map[string]bool),
		stopCh:         make(chan struct{}),
	}
	for _, id := range ring.IDs() {
		m.alive[id] = true
	}
	hub.mu.Lock()
	hub.nodes[cfg.NodeID] = m
	hub.mu.Unlock()
	return m, nil
}

func (m *MemoryMesh) Start(ctx context.Context) error {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	// Exchange directory snapshots with peers.
	m.hub.mu.RLock()
	peers := make([]*MemoryMesh, 0, len(m.hub.nodes))
	for id, n := range m.hub.nodes {
		if id != m.cfg.NodeID {
			peers = append(peers, n)
		}
	}
	m.hub.mu.RUnlock()
	snap := m.dir.Snapshot()
	for _, p := range peers {
		p.dir.ApplySnapshot(snap)
		m.dir.ApplySnapshot(p.dir.Snapshot())
	}
	return nil
}

func (m *MemoryMesh) Stop() error {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.hub.mu.Lock()
	delete(m.hub.nodes, m.cfg.NodeID)
	m.hub.mu.Unlock()
	return nil
}

func (m *MemoryMesh) NodeID() string    { return m.cfg.NodeID }
func (m *MemoryMesh) PeerIDs() []string { return m.ring.IDs() }
func (m *MemoryMesh) OwnerNode(cs string) string {
	return m.ring.Owner(cs)
}
func (m *MemoryMesh) PeerDeathGrace() time.Duration { return m.peerDeathGrace }
func (m *MemoryMesh) ClaimTimeout() time.Duration   { return m.claimTimeout }

func (m *MemoryMesh) peer(nodeID string) *MemoryMesh {
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	return m.hub.nodes[nodeID]
}

func (m *MemoryMesh) withDelay(fn func()) {
	d := m.hub.delayCopy()
	if d <= 0 {
		fn()
		return
	}
	time.AfterFunc(d, fn)
}

// SimulatePeerDown marks a peer dead and after grace runs OnPeerDead cleanup.
func (m *MemoryMesh) SimulatePeerDown(nodeID string) {
	m.deadMu.Lock()
	m.alive[nodeID] = false
	m.deadMu.Unlock()
	grace := m.peerDeathGrace
	go func() {
		select {
		case <-time.After(grace):
		case <-m.stopCh:
			return
		}
		// Capture DirMeta (incl IsATC) BEFORE directory remove (issue 6).
		seen := map[string]DirMeta{}
		m.hub.mu.RLock()
		nodes := make([]*MemoryMesh, 0, len(m.hub.nodes))
		for _, n := range m.hub.nodes {
			nodes = append(nodes, n)
		}
		m.hub.mu.RUnlock()
		for _, n := range nodes {
			for _, meta := range n.dir.Snapshot() {
				if meta.NodeID == nodeID {
					seen[stringsToUpper(meta.Callsign)] = meta
				}
			}
		}
		metas := make([]DirMeta, 0, len(seen))
		for _, meta := range seen {
			metas = append(metas, meta)
		}
		m.claims.ReleaseNode(nodeID)
		for _, n := range nodes {
			n.claims.ReleaseNode(nodeID)
			n.dir.RemoveNode(nodeID)
		}
		if m.onPeerDead != nil {
			m.onPeerDead(nodeID, metas)
		}
	}()
}

// ForcePeerDeadImmediate is like SimulatePeerDown with zero grace (tests).
func (m *MemoryMesh) ForcePeerDeadImmediate(nodeID string) {
	old := m.peerDeathGrace
	m.peerDeathGrace = 0
	m.SimulatePeerDown(nodeID)
	m.peerDeathGrace = old
	// wait a tick
	time.Sleep(10 * time.Millisecond)
}

func (m *MemoryMesh) isAlive(nodeID string) bool {
	m.deadMu.Lock()
	defer m.deadMu.Unlock()
	a, ok := m.alive[nodeID]
	return ok && a
}

func (m *MemoryMesh) ClaimReserve(ctx context.Context, callsign string, meta ClaimMeta) (string, error) {
	owner := m.ring.Owner(callsign)
	if meta.NodeID == "" {
		meta.NodeID = m.cfg.NodeID
	}
	timeout := m.claimTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}
	type result struct {
		fence string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		m.withDelay(func() {
			if owner != m.cfg.NodeID && !m.isAlive(owner) {
				ch <- result{"", ErrPeerDown}
				return
			}
			target := m
			if owner != m.cfg.NodeID {
				p := m.peer(owner)
				if p == nil || !m.isAlive(owner) {
					ch <- result{"", ErrPeerDown}
					return
				}
				target = p
			}
			fence, err := target.claims.Reserve(callsign, meta)
			ch <- result{fence, err}
		})
	}()
	select {
	case <-ctx.Done():
		return "", ErrClaimTimeout
	case <-time.After(timeout):
		return "", ErrClaimTimeout
	case r := <-ch:
		return r.fence, r.err
	}
}

func (m *MemoryMesh) ClaimCommit(ctx context.Context, callsign, fence string) error {
	owner := m.ring.Owner(callsign)
	timeout := m.claimTimeout
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		m.withDelay(func() {
			if owner != m.cfg.NodeID && !m.isAlive(owner) {
				ch <- result{ErrPeerDown}
				return
			}
			target := m
			if owner != m.cfg.NodeID {
				p := m.peer(owner)
				if p == nil {
					ch <- result{ErrPeerDown}
					return
				}
				target = p
			}
			meta, err := target.claims.Commit(callsign, fence)
			if err != nil {
				ch <- result{err}
				return
			}
			// DirectoryDelta join on all nodes
			m.broadcastDirJoin(meta)
			ch <- result{nil}
		})
	}()
	select {
	case <-ctx.Done():
		return ErrClaimTimeout
	case <-time.After(timeout):
		return ErrClaimTimeout
	case r := <-ch:
		return r.err
	}
}

func (m *MemoryMesh) broadcastDirJoin(meta DirMeta) {
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		n.dir.ApplyJoin(meta)
	}
}

func (m *MemoryMesh) broadcastDirLeave(callsign string) {
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		n.dir.ApplyLeave(callsign)
	}
}

func (m *MemoryMesh) ClaimAbort(callsign, fence string) {
	owner := m.ring.Owner(callsign)
	target := m
	if owner != m.cfg.NodeID {
		if p := m.peer(owner); p != nil {
			target = p
		}
	}
	target.claims.Abort(callsign, fence)
}

func (m *MemoryMesh) ClaimRelease(callsign, fence string) {
	owner := m.ring.Owner(callsign)
	target := m
	if owner != m.cfg.NodeID {
		if p := m.peer(owner); p != nil {
			target = p
		}
	}
	if target.claims.Release(callsign, fence) {
		m.broadcastDirLeave(callsign)
	}
}

func (m *MemoryMesh) Lookup(callsign string) (string, DirMeta, bool) {
	return m.dir.Lookup(callsign)
}

func (m *MemoryMesh) SendDirect(callsign string, wire []byte) error {
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
	p := m.peer(nodeID)
	if p == nil || !m.isAlive(nodeID) {
		return ErrPeerDown
	}
	from := m.cfg.NodeID
	w := append([]byte(nil), wire...)
	m.withDelay(func() {
		if p.onDirectWire != nil {
			p.onDirectWire(from, w, WireDirect)
		}
	})
	return nil
}

func (m *MemoryMesh) HomeRPC(ctx context.Context, callsign string, op HomeOp, payload []byte) ([]byte, error) {
	nodeID, _, ok := m.dir.Lookup(callsign)
	if !ok {
		return nil, ErrHomeRPCNotFound
	}
	target := m
	if nodeID != m.cfg.NodeID {
		p := m.peer(nodeID)
		if p == nil || !m.isAlive(nodeID) {
			return nil, ErrPeerDown
		}
		target = p
	}
	timeout := m.claimTimeout
	if timeout < time.Second {
		timeout = time.Second
	}
	type result struct {
		resp []byte
		err  error
	}
	ch := make(chan result, 1)
	pl := append([]byte(nil), payload...)
	go func() {
		m.withDelay(func() {
			if target.onHomeRPC == nil {
				ch <- result{nil, ErrHomeRPCNotFound}
				return
			}
			resp, err := target.onHomeRPC(op, callsign, pl)
			ch <- result{resp, err}
		})
	}()
	select {
	case <-ctx.Done():
		return nil, ErrHomeRPCTimeout
	case <-time.After(timeout):
		return nil, ErrHomeRPCTimeout
	case r := <-ch:
		return r.resp, r.err
	}
}

func (m *MemoryMesh) ForwardTextRanged(wire []byte, senderBoxes []AABB) {
	// Non-blocking text fan-out (async withDelay); payload includes sender boxes for re-fan.
	m.mu.RLock()
	interests := make(map[string]InterestSummary, len(m.interest))
	for k, v := range m.interest {
		interests[k] = v
	}
	m.mu.RUnlock()
	from := m.cfg.NodeID
	w := SanitizeWireBytes(append([]byte(nil), wire...))
	// Encode boxes + wire (same layout as TCPMesh).
	var payload []byte
	payload = encodeU32(payload, uint32(len(senderBoxes)))
	for _, a := range senderBoxes {
		payload = encodeF64(payload, a.MinLat)
		payload = encodeF64(payload, a.MinLon)
		payload = encodeF64(payload, a.MaxLat)
		payload = encodeF64(payload, a.MaxLon)
	}
	payload = append(payload, w...)

	m.hub.mu.RLock()
	peers := make([]*MemoryMesh, 0, len(m.hub.nodes))
	for id, n := range m.hub.nodes {
		if id == m.cfg.NodeID {
			continue
		}
		peers = append(peers, n)
	}
	m.hub.mu.RUnlock()
	for _, p := range peers {
		sum := interests[p.cfg.NodeID]
		if len(sum.Boxes) > 0 && !AnyAABBOverlap(senderBoxes, sum.Boxes) {
			p.mu.RLock()
			localSum := p.interest[p.cfg.NodeID]
			p.mu.RUnlock()
			if len(localSum.Boxes) > 0 && !AnyAABBOverlap(senderBoxes, localSum.Boxes) {
				continue
			}
		}
		peer := p
		pl := append([]byte(nil), payload...)
		m.withDelay(func() {
			if peer.onDirectWire != nil {
				peer.onDirectWire(from, pl, WireRanged)
			}
		})
	}
}

// DecodeTextRangedPayload splits TypeTextRanged payload into sender boxes + wire.
func DecodeTextRangedPayload(p []byte) (boxes []AABB, wire []byte, err error) {
	n, rest, err := decodeU32(p)
	if err != nil {
		return nil, nil, err
	}
	for i := uint32(0); i < n; i++ {
		var a AABB
		a.MinLat, rest, err = decodeF64(rest)
		if err != nil {
			return nil, nil, err
		}
		a.MinLon, rest, err = decodeF64(rest)
		if err != nil {
			return nil, nil, err
		}
		a.MaxLat, rest, err = decodeF64(rest)
		if err != nil {
			return nil, nil, err
		}
		a.MaxLon, rest, err = decodeF64(rest)
		if err != nil {
			return nil, nil, err
		}
		boxes = append(boxes, a)
	}
	return boxes, rest, nil
}

func (m *MemoryMesh) ForwardRanged(wire []byte, senderBoxes []AABB, class RangeClass, senderCallsign string) {
	m.mu.RLock()
	interests := make(map[string]InterestSummary, len(m.interest))
	for k, v := range m.interest {
		interests[k] = v
	}
	m.mu.RUnlock()

	from := m.cfg.NodeID
	w := SanitizeWireBytes(append([]byte(nil), wire...))
	boxes := append([]AABB(nil), senderBoxes...)
	batch := PositionBatch{
		SenderCallsign: SanitizeMeshString(senderCallsign, 32),
		SenderBoxes:    boxes,
		Velocity:       class == RangeClassVelocity,
		Wires:          [][]byte{w},
	}

	m.hub.mu.RLock()
	peers := make([]*MemoryMesh, 0, len(m.hub.nodes))
	for id, n := range m.hub.nodes {
		if id == m.cfg.NodeID {
			continue
		}
		peers = append(peers, n)
	}
	m.hub.mu.RUnlock()

	for _, p := range peers {
		sum, ok := interests[p.cfg.NodeID]
		if !ok || !AnyAABBOverlap(boxes, sum.Boxes) {
			// Also check peer's published interest stored on peer
			p.mu.RLock()
			localSum := p.interest[p.cfg.NodeID]
			p.mu.RUnlock()
			if len(localSum.Boxes) == 0 || !AnyAABBOverlap(boxes, localSum.Boxes) {
				// if no interest yet, skip (cold start: optional flood avoided)
				if len(sum.Boxes) == 0 && len(localSum.Boxes) == 0 {
					// forward anyway on empty interest so tests with positions work before interest publish
					// Design: over-forward OK. Cold mesh: forward to all peers.
				} else {
					continue
				}
			}
		}
		peer := p
		m.withDelay(func() {
			if peer.onPositionBatch != nil {
				peer.onPositionBatch(from, batch)
			}
		})
	}
}

func (m *MemoryMesh) PublishInterest(sum InterestSummary) {
	if sum.NodeID == "" {
		sum.NodeID = m.cfg.NodeID
	}
	m.mu.Lock()
	m.interest[sum.NodeID] = sum
	m.mu.Unlock()
	// fan to peers
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		if n.cfg.NodeID == m.cfg.NodeID {
			continue
		}
		n.mu.Lock()
		n.interest[sum.NodeID] = sum
		n.mu.Unlock()
	}
}

func (m *MemoryMesh) BroadcastJoinLeave(wire []byte) {
	from := m.cfg.NodeID
	w := append([]byte(nil), wire...)
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		if n.cfg.NodeID == m.cfg.NodeID {
			continue
		}
		peer := n
		m.withDelay(func() {
			if peer.onDirectWire != nil {
				peer.onDirectWire(from, w, WireJoinLeave)
			}
		})
	}
}

func (m *MemoryMesh) BroadcastClass(wire []byte, class BroadcastClass) {
	from := m.cfg.NodeID
	w := SanitizeWireBytes(append([]byte(nil), wire...))
	// Encode class in first byte of a wrapper is not used — class passed via WireClass + callback.
	// Receivers use OnDirectWire with WireBroadcast; server re-fan must honor class.
	// We piggyback class as WireClass subtypes by packing into a small header for MemoryMesh:
	// first byte = BroadcastClass, rest = wire. TCP mesh does the same.
	payload := append([]byte{byte(class)}, w...)
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		if n.cfg.NodeID == m.cfg.NodeID {
			continue
		}
		peer := n
		m.withDelay(func() {
			if peer.onDirectWire != nil {
				peer.onDirectWire(from, payload, WireBroadcast)
			}
		})
	}
}

func (m *MemoryMesh) NotifyLocalJoin(meta DirMeta) {
	if meta.NodeID == "" {
		meta.NodeID = m.cfg.NodeID
	}
	meta.Routable = true
	m.broadcastDirJoin(meta)
}

func (m *MemoryMesh) NotifyLocalLeave(callsign string) {
	m.broadcastDirLeave(callsign)
}

func (m *MemoryMesh) NotifyLocalFPL(callsign, fplInfo string) {
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		n.dir.ApplyMeta(callsign, &fplInfo, nil)
	}
}

func (m *MemoryMesh) NotifyLocalBeacon(callsign, beacon string) {
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	for _, n := range m.hub.nodes {
		n.dir.ApplyMeta(callsign, nil, &beacon)
	}
}

func (m *MemoryMesh) OnPeerDead(fn func(nodeID string, metas []DirMeta)) {
	m.onPeerDead = fn
}
func (m *MemoryMesh) OnDirectWire(fn func(fromNode string, wire []byte, class WireClass)) {
	m.onDirectWire = fn
}
func (m *MemoryMesh) OnHomeRPC(fn func(op HomeOp, callsign string, payload []byte) ([]byte, error)) {
	m.onHomeRPC = fn
}
func (m *MemoryMesh) OnPositionBatch(fn func(fromNode string, batch PositionBatch)) {
	m.onPositionBatch = fn
}
func (m *MemoryMesh) OnProximityHint(fn func(fromNode string, targetCallsign string, distanceM float64)) {
	m.onProximityHint = fn
}

// SendProximityHint notifies the home node of a remote closest distance for $SF.
func (m *MemoryMesh) SendProximityHint(homeNode, targetCallsign string, distanceM float64) {
	if homeNode == "" || homeNode == m.cfg.NodeID {
		if m.onProximityHint != nil {
			m.onProximityHint(m.cfg.NodeID, targetCallsign, distanceM)
		}
		return
	}
	p := m.peer(homeNode)
	if p == nil {
		return
	}
	from := m.cfg.NodeID
	cs := SanitizeMeshString(targetCallsign, 32)
	m.withDelay(func() {
		if p.onProximityHint != nil {
			p.onProximityHint(from, cs, distanceM)
		}
	})
}

// Ensure MemoryMesh implements Mesh.
var _ Mesh = (*MemoryMesh)(nil)

// MinFloat is used by $SF aggregate helpers.
func MinFloat(a, b float64) float64 {
	return math.Min(a, b)
}

// NewFence generates a claim fence token.
func NewFence() string {
	return uuid.NewString()
}
