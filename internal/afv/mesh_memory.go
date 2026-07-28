package afv

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

// MemoryHub is a shared in-process fabric for MemoryMesh nodes (tests only).
type MemoryHub struct {
	mu    sync.RWMutex
	nodes map[string]*MemoryMesh
}

// NewMemoryHub creates an empty hub.
func NewMemoryHub() *MemoryHub {
	return &MemoryHub{nodes: make(map[string]*MemoryMesh)}
}

// MemoryMesh implements Mesh in-process for two-node e2e tests.
// Interest apply is synchronous (M-17): after peer.PublishInterest returns,
// local PeerWants already reflects the new set.
type MemoryMesh struct {
	cfg  MeshConfig
	hub  *MemoryHub
	psk  string
	self string

	mu      sync.RWMutex
	started bool
	// peerID → set of FreqCell
	peerInterest map[string]map[FreqCell]struct{}
	// peer liveness
	alive map[string]bool

	// per-peer outbound queues (test drop-oldest paths)
	peerVoice map[string]*dropOldestQueue[AudioRelay]
	peerCtrl  map[string]*dropOldestQueue[meshCtrlJob]

	// drop counters
	voiceDrops atomic.Uint64
	ctrlDrops  atomic.Uint64

	onAudio    func(fromNode string, r AudioRelay)
	onPeerDead func(nodeID string)
	onDir      MeshDirectoryHandler
	snapFn     func() []MeshSessionBlock
	interestFn func() []InterestEntry

	// capture last outbound AudioRelay encodings for Case E (optional)
	lastRelayMu sync.Mutex
	lastRelays  []AudioRelay

	// interestPublishCount counts PublishInterest calls (rate tests).
	interestPublishCount atomic.Uint64

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

type meshCtrlJob struct {
	typ     byte
	payload []byte
}

// NewMemoryMesh registers a node on hub. PeerIDs may include self.
func NewMemoryMesh(hub *MemoryHub, cfg MeshConfig) (*MemoryMesh, error) {
	if hub == nil {
		return nil, fmt.Errorf("afv mesh: nil hub")
	}
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, fmt.Errorf("afv mesh: empty NodeID")
	}
	m := &MemoryMesh{
		cfg:          cfg,
		hub:          hub,
		psk:          cfg.PSK,
		self:         cfg.NodeID,
		peerInterest: make(map[string]map[FreqCell]struct{}),
		alive:        make(map[string]bool),
		peerVoice:    make(map[string]*dropOldestQueue[AudioRelay]),
		peerCtrl:     make(map[string]*dropOldestQueue[meshCtrlJob]),
		stopCh:       make(chan struct{}),
	}
	for _, id := range cfg.PeerIDs {
		if id == "" || id == cfg.NodeID {
			continue
		}
		m.alive[id] = true
		m.peerVoice[id] = newDropOldestQueue[AudioRelay](meshVoiceQueueDepth)
		m.peerCtrl[id] = newDropOldestQueue[meshCtrlJob](meshControlQueueDepth)
		m.peerInterest[id] = make(map[FreqCell]struct{})
	}
	hub.mu.Lock()
	if hub.nodes == nil {
		hub.nodes = make(map[string]*MemoryMesh)
	}
	if _, exists := hub.nodes[cfg.NodeID]; exists {
		hub.mu.Unlock()
		return nil, fmt.Errorf("afv mesh: node %q already registered", cfg.NodeID)
	}
	hub.nodes[cfg.NodeID] = m
	hub.mu.Unlock()
	return m, nil
}

func (m *MemoryMesh) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("afv mesh: nil mesh")
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	// Auth first (M-3 / Issue 2+9): fail closed before workers or Snapshot/Interest.
	m.hub.mu.RLock()
	for id, peer := range m.hub.nodes {
		if id == m.self {
			continue
		}
		if err := VerifyHelloPSK(m.psk, peer.psk); err != nil {
			m.hub.mu.RUnlock()
			slog.Warn("AFV mesh hello auth failed", "peer", id)
			return err
		}
	}
	m.hub.mu.RUnlock()

	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	// reset stop so a prior failed Start/Stop can recover in tests
	m.mu.Unlock()

	// Start per-peer drainers that deliver control then voice.
	m.mu.RLock()
	peers := make([]string, 0, len(m.peerVoice))
	for id := range m.peerVoice {
		peers = append(peers, id)
	}
	m.mu.RUnlock()
	for _, pid := range peers {
		pid := pid
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.drainPeer(ctx, pid)
		}()
	}

	// Post-auth sequence (M-16): Snapshot then current Interest (may be empty).
	m.PublishTrxSnapshot()
	m.publishCurrentInterest()

	go func() {
		<-ctx.Done()
		_ = m.Stop()
	}()
	return nil
}

// publishCurrentInterest sends Interest from provider or empty set (M-16).
func (m *MemoryMesh) publishCurrentInterest() {
	m.mu.RLock()
	fn := m.interestFn
	m.mu.RUnlock()
	var entries []InterestEntry
	if fn != nil {
		entries = fn()
	}
	m.PublishInterest(entries)
}

func (m *MemoryMesh) Stop() error {
	if m == nil {
		return nil
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
		m.hub.mu.Lock()
		delete(m.hub.nodes, m.self)
		m.hub.mu.Unlock()
		m.mu.Lock()
		m.started = false
		m.mu.Unlock()
	})
	m.wg.Wait()
	return nil
}

func (m *MemoryMesh) NodeID() string {
	if m == nil {
		return ""
	}
	return m.self
}

func (m *MemoryMesh) OnAudioRelay(fn func(fromNode string, r AudioRelay)) {
	m.mu.Lock()
	m.onAudio = fn
	m.mu.Unlock()
}

func (m *MemoryMesh) OnPeerDead(fn func(nodeID string)) {
	m.mu.Lock()
	m.onPeerDead = fn
	m.mu.Unlock()
}

func (m *MemoryMesh) OnDirectory(fn MeshDirectoryHandler) {
	m.mu.Lock()
	m.onDir = fn
	m.mu.Unlock()
}

func (m *MemoryMesh) SetSnapshotProvider(fn func() []MeshSessionBlock) {
	m.mu.Lock()
	m.snapFn = fn
	m.mu.Unlock()
}

func (m *MemoryMesh) SetInterestProvider(fn func() []InterestEntry) {
	m.mu.Lock()
	m.interestFn = fn
	m.mu.Unlock()
}

// InterestPublishCount returns how many times PublishInterest ran (tests).
func (m *MemoryMesh) InterestPublishCount() uint64 {
	return m.interestPublishCount.Load()
}

func (m *MemoryMesh) peerAlive(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.alive[id]
}

// SimulatePeerDown marks peer dead, clears interest, notifies OnPeerDead (tests Case D).
func (m *MemoryMesh) SimulatePeerDown(nodeID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.alive[nodeID] = false
	delete(m.peerInterest, nodeID)
	m.peerInterest[nodeID] = make(map[FreqCell]struct{})
	cb := m.onPeerDead
	m.mu.Unlock()
	if cb != nil {
		cb(nodeID)
	}
}

// SimulatePeerUp re-marks peer alive and re-runs post-Hello Snapshot+Interest (Case D).
func (m *MemoryMesh) SimulatePeerUp(nodeID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.alive[nodeID] = true
	if m.peerInterest[nodeID] == nil {
		m.peerInterest[nodeID] = make(map[FreqCell]struct{})
	}
	if m.peerVoice[nodeID] == nil {
		m.peerVoice[nodeID] = newDropOldestQueue[AudioRelay](meshVoiceQueueDepth)
	}
	if m.peerCtrl[nodeID] == nil {
		m.peerCtrl[nodeID] = newDropOldestQueue[meshCtrlJob](meshControlQueueDepth)
	}
	m.mu.Unlock()
	// Post-Hello sequence with current Interest (M-16), not forced empty.
	m.PublishTrxSnapshot()
	m.publishCurrentInterest()
}

// ClearPeerInterest empties a peer's interest set (Case F / tests).
func (m *MemoryMesh) ClearPeerInterest(peerID string) {
	m.mu.Lock()
	m.peerInterest[peerID] = make(map[FreqCell]struct{})
	m.mu.Unlock()
}

// VoiceDrops returns voice queue drop count.
func (m *MemoryMesh) VoiceDrops() uint64 { return m.voiceDrops.Load() }

// CtrlDrops returns control queue drop count.
func (m *MemoryMesh) CtrlDrops() uint64 { return m.ctrlDrops.Load() }

// LastRelays returns copies of recently enqueued AudioRelays (Case E).
func (m *MemoryMesh) LastRelays() []AudioRelay {
	m.lastRelayMu.Lock()
	defer m.lastRelayMu.Unlock()
	out := make([]AudioRelay, len(m.lastRelays))
	copy(out, m.lastRelays)
	return out
}

func (m *MemoryMesh) PublishTrxSnapshot() {
	if m == nil {
		return
	}
	m.mu.RLock()
	snapFn := m.snapFn
	m.mu.RUnlock()
	var sessions []MeshSessionBlock
	if snapFn != nil {
		sessions = snapFn()
	}
	payload := EncodeTrxSnapshot(TrxSnapshotPayload{
		OriginNodeID: m.self,
		Sessions:     sessions,
	})
	m.broadcastCtrl(MeshTypeTrxSnapshot, payload)
}

func (m *MemoryMesh) PublishTrxDelta(callsign string, isATC bool, trxs []Transceiver) {
	if m == nil {
		return
	}
	meshTrxs := make([]MeshTrx, 0, len(trxs))
	for _, t := range trxs {
		meshTrxs = append(meshTrxs, MeshTrx{
			ID: t.ID, FreqHz: t.Frequency,
			LatDeg: t.LatDeg, LonDeg: t.LonDeg, AltM: t.HeightMslM,
		})
	}
	payload := EncodeTrxDelta(TrxDeltaPayload{
		OriginNodeID: m.self,
		Callsign:     callsign,
		IsATC:        isATC,
		Trxs:         meshTrxs,
	})
	m.broadcastCtrl(MeshTypeTrxDelta, payload)
}

func (m *MemoryMesh) PublishSessionLeave(callsign string) {
	if m == nil {
		return
	}
	payload := EncodeSessionLeave(SessionLeavePayload{
		OriginNodeID: m.self,
		Callsign:     callsign,
	})
	m.broadcastCtrl(MeshTypeSessionLeave, payload)
}

func (m *MemoryMesh) PublishInterest(entries []InterestEntry) {
	if m == nil {
		return
	}
	m.interestPublishCount.Add(1)
	// Synchronous apply on peers only (M-17). Control queue carries the frame for
	// transport/drop-oldest metrics; deliverCtrl must NOT re-apply Interest (stale
	// async frames must not overwrite a newer sync set).
	payload := EncodeInterest(InterestPayload{NodeID: m.self, Entries: entries})
	set := make(map[FreqCell]struct{}, len(entries))
	for _, e := range entries {
		set[FreqCell{FreqHz: e.FreqHz, Cell: geo.CellKey{ILat: e.ILat, ILon: e.ILon}}] = struct{}{}
	}

	m.hub.mu.RLock()
	peers := make([]*MemoryMesh, 0, len(m.hub.nodes))
	for id, p := range m.hub.nodes {
		if id == m.self {
			continue
		}
		peers = append(peers, p)
	}
	m.hub.mu.RUnlock()

	for _, p := range peers {
		if !m.peerAlive(p.self) {
			continue
		}
		// Apply interest on peer: "peer m wants these cells" from p's view
		p.mu.Lock()
		if p.peerInterest == nil {
			p.peerInterest = make(map[string]map[FreqCell]struct{})
		}
		cp := make(map[FreqCell]struct{}, len(set))
		for k := range set {
			cp[k] = struct{}{}
		}
		p.peerInterest[m.self] = cp
		p.mu.Unlock()
	}
	// Enqueue for control-path drop-oldest only — not applied on receive (Issue 1).
	m.broadcastCtrl(MeshTypeInterest, payload)
}

func (m *MemoryMesh) EnqueueAudioRelay(relay AudioRelay) {
	if m == nil {
		return
	}
	// Private copy of audio
	if relay.Audio != nil {
		relay.Audio = append([]byte(nil), relay.Audio...)
	}
	if relay.TxRadios != nil {
		relay.TxRadios = append([]RelayTxRadio(nil), relay.TxRadios...)
	}
	if relay.OriginNode == "" {
		relay.OriginNode = m.self
	}

	m.lastRelayMu.Lock()
	m.lastRelays = append(m.lastRelays, relay)
	if len(m.lastRelays) > 32 {
		m.lastRelays = m.lastRelays[len(m.lastRelays)-32:]
	}
	m.lastRelayMu.Unlock()

	m.mu.RLock()
	peerIDs := make([]string, 0, len(m.peerVoice))
	for id := range m.peerVoice {
		if m.alive[id] {
			peerIDs = append(peerIDs, id)
		}
	}
	m.mu.RUnlock()

	for _, pid := range peerIDs {
		want := false
		for _, tx := range relay.TxRadios {
			ck := geo.CellKey{
				ILat: geo.CellIndex(tx.LatDeg, geo.DefaultGridCellDeg),
				ILon: geo.CellIndex(tx.LonDeg, geo.DefaultGridCellDeg),
			}
			if m.PeerWants(pid, tx.FreqHz, ck) {
				want = true
				break
			}
		}
		if !want {
			continue // no flood when Interest empty
		}
		m.mu.RLock()
		q := m.peerVoice[pid]
		m.mu.RUnlock()
		if q == nil {
			continue
		}
		// copy per peer
		cp := relay
		cp.Audio = append([]byte(nil), relay.Audio...)
		cp.TxRadios = append([]RelayTxRadio(nil), relay.TxRadios...)
		if q.Enqueue(cp) {
			m.voiceDrops.Add(1)
		}
	}
}

func (m *MemoryMesh) PeerWants(peerID string, freqHz uint32, cell geo.CellKey) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := m.peerInterest[peerID]
	if len(set) == 0 {
		return false
	}
	_, ok := set[FreqCell{FreqHz: freqHz, Cell: cell}]
	return ok
}

func (m *MemoryMesh) InterestedPeers(keys []FreqCell) []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []string
	for pid, set := range m.peerInterest {
		if !m.alive[pid] || len(set) == 0 {
			continue
		}
		for _, k := range keys {
			if _, ok := set[k]; ok {
				out = append(out, pid)
				break
			}
		}
	}
	return out
}

func (m *MemoryMesh) broadcastCtrl(typ byte, payload []byte) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, q := range m.peerCtrl {
		if !m.alive[id] || q == nil {
			continue
		}
		job := meshCtrlJob{typ: typ, payload: append([]byte(nil), payload...)}
		if q.Enqueue(job) {
			m.ctrlDrops.Add(1)
			slog.Debug("AFV mesh control drop", "peer", id, "type", typ)
		}
	}
}

func (m *MemoryMesh) drainPeer(ctx context.Context, peerID string) {
	idle := time.NewTimer(2 * time.Millisecond)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		default:
		}
		// Priority: control then voice
		m.mu.RLock()
		cq := m.peerCtrl[peerID]
		vq := m.peerVoice[peerID]
		alive := m.alive[peerID]
		m.mu.RUnlock()
		if !alive {
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(50 * time.Millisecond)
			select {
			case <-ctx.Done():
				return
			case <-m.stopCh:
				return
			case <-idle.C:
			}
			continue
		}
		delivered := false
		if cq != nil {
			if job, ok := cq.TryRecv(); ok {
				m.deliverCtrl(peerID, job)
				delivered = true
			}
		}
		if !delivered && vq != nil {
			if relay, ok := vq.TryRecv(); ok {
				m.deliverVoice(peerID, relay)
				delivered = true
			}
		}
		if !delivered {
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(2 * time.Millisecond)
			select {
			case <-ctx.Done():
				return
			case <-m.stopCh:
				return
			case <-idle.C:
			}
		}
	}
}

func (m *MemoryMesh) deliverCtrl(peerID string, job meshCtrlJob) {
	peer := m.lookupPeer(peerID)
	if peer == nil {
		return
	}
	switch job.typ {
	case MeshTypeTrxSnapshot:
		p, err := DecodeTrxSnapshot(job.payload)
		if err != nil {
			slog.Debug("AFV mesh decode TrxSnapshot", "err", err, "from", m.self, "to", peerID)
			return
		}
		sessions := meshSessionsToRemote(p.Sessions)
		peer.mu.RLock()
		dir := peer.onDir
		peer.mu.RUnlock()
		if dir != nil {
			dir.ApplySnapshot(p.OriginNodeID, sessions)
		}
	case MeshTypeTrxDelta:
		p, err := DecodeTrxDelta(job.payload)
		if err != nil {
			slog.Debug("AFV mesh decode TrxDelta", "err", err, "from", m.self)
			return
		}
		trxs := meshTrxToLocal(p.Trxs)
		peer.mu.RLock()
		dir := peer.onDir
		peer.mu.RUnlock()
		if dir != nil {
			dir.ApplyDelta(p.OriginNodeID, p.Callsign, p.IsATC, trxs)
		}
	case MeshTypeSessionLeave:
		p, err := DecodeSessionLeave(job.payload)
		if err != nil {
			slog.Debug("AFV mesh decode SessionLeave", "err", err, "from", m.self)
			return
		}
		peer.mu.RLock()
		dir := peer.onDir
		peer.mu.RUnlock()
		if dir != nil {
			dir.ApplyLeave(p.OriginNodeID, p.Callsign)
		}
	case MeshTypeInterest:
		// Interest is applied only synchronously in PublishInterest (M-17).
		// Ignoring async re-apply prevents older queued frames from overwriting
		// a newer set (Issue 1). Frame still counts toward control-queue metrics.
		return
	case MeshTypeHeartbeat:
		// liveness only
	}
}

func (m *MemoryMesh) deliverVoice(peerID string, relay AudioRelay) {
	peer := m.lookupPeer(peerID)
	if peer == nil {
		return
	}
	peer.mu.RLock()
	fn := peer.onAudio
	peer.mu.RUnlock()
	if fn != nil {
		fn(m.self, relay)
	}
}

func (m *MemoryMesh) lookupPeer(id string) *MemoryMesh {
	m.hub.mu.RLock()
	defer m.hub.mu.RUnlock()
	return m.hub.nodes[id]
}

func meshSessionsToRemote(sessions []MeshSessionBlock) []RemoteSession {
	out := make([]RemoteSession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, RemoteSession{
			Callsign: s.Callsign,
			IsATC:    s.IsATC,
			Trxs:     meshTrxToLocal(s.Trxs),
		})
	}
	return out
}

func meshTrxToLocal(trxs []MeshTrx) []Transceiver {
	out := make([]Transceiver, 0, len(trxs))
	for _, t := range trxs {
		out = append(out, Transceiver{
			ID: t.ID, Frequency: t.FreqHz,
			LatDeg: t.LatDeg, LonDeg: t.LonDeg, HeightMslM: t.AltM,
		})
	}
	return out
}

// ForceEnqueueVoiceForTest fills voice queue for drop-oldest unit tests.
func (m *MemoryMesh) ForceEnqueueVoiceForTest(peerID string, n int, relay AudioRelay) {
	m.mu.RLock()
	q := m.peerVoice[peerID]
	m.mu.RUnlock()
	if q == nil {
		return
	}
	for i := 0; i < n; i++ {
		cp := relay
		cp.SequenceCounter = uint32(i)
		cp.Audio = append([]byte(nil), relay.Audio...)
		if q.Enqueue(cp) {
			m.voiceDrops.Add(1)
		}
	}
}

// ForceEnqueueCtrlForTest fills control queue for drop-oldest unit tests.
func (m *MemoryMesh) ForceEnqueueCtrlForTest(peerID string, n int) {
	m.mu.RLock()
	q := m.peerCtrl[peerID]
	m.mu.RUnlock()
	if q == nil {
		return
	}
	for i := 0; i < n; i++ {
		job := meshCtrlJob{typ: MeshTypeHeartbeat, payload: EncodeHeartbeatPayload(uint64(i))}
		if q.Enqueue(job) {
			m.ctrlDrops.Add(1)
		}
	}
}

// PeerVoiceQueueLen returns buffered voice jobs for peer (tests).
func (m *MemoryMesh) PeerVoiceQueueLen(peerID string) int {
	m.mu.RLock()
	q := m.peerVoice[peerID]
	m.mu.RUnlock()
	if q == nil {
		return 0
	}
	return q.Len()
}

// ApplyInterestDirect sets local view of peer interest (tests / sync helper).
func (m *MemoryMesh) ApplyInterestDirect(fromPeer string, entries []InterestEntry) {
	set := make(map[FreqCell]struct{}, len(entries))
	for _, e := range entries {
		set[FreqCell{FreqHz: e.FreqHz, Cell: geo.CellKey{ILat: e.ILat, ILon: e.ILon}}] = struct{}{}
	}
	m.mu.Lock()
	m.peerInterest[fromPeer] = set
	m.mu.Unlock()
}
