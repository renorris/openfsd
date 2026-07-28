package afv

// HybridMesh is the production AFV multi-node fabric (PR-10b):
// TCP control plane (Hello, HB, Trx*, Leave, Interest) + UDP voice (AudioRelay only).
// MemoryMesh remains tests-only.
//
// Lifecycle: single Start/Stop per instance (not restartable after Stop).

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

// HybridMeshConfig configures production hybrid mesh.
type HybridMeshConfig struct {
	NodeID      string
	ListenTCP   string // AFV_CLUSTER_LISTEN
	ListenVoice string // AFV_CLUSTER_VOICE_LISTEN
	Peers       []ClusterPeer
	PSK         string

	HeartbeatInterval time.Duration // default 2s
	PeerDeathAfter    time.Duration // default 15s
	CtrlQueueDepth    int           // default 64
	VoiceQueueDepth   int           // default 256
	Logger            *slog.Logger
}

// HybridMesh implements Mesh over TCP control + UDP AudioRelay voice.
type HybridMesh struct {
	cfg            HybridMeshConfig
	peerDeathGrace time.Duration
	hbInterval     time.Duration
	ctrlDepth      int
	voiceDepth     int
	logger         *slog.Logger

	// peerID → static config
	peerCfg map[string]ClusterPeer

	mu sync.RWMutex
	// peerInterest filled only from inbound TCP Interest (H-14)
	peerInterest map[string]map[FreqCell]struct{}
	peers        map[string]*hybridPeer

	// UDP allowlist: canonical key → peerID
	allowlist map[string]string
	// resolved send targets
	sendVoice map[string]*net.UDPAddr

	ln      net.Listener
	voicePC net.PacketConn

	onAudio    func(fromNode string, r AudioRelay)
	onPeerDead func(nodeID string)
	onDir      MeshDirectoryHandler
	snapFn     func() []MeshSessionBlock
	interestFn func() []InterestEntry

	// counters
	voiceTx          atomic.Uint64
	voiceRx          atomic.Uint64
	udpAllowDrops    atomic.Uint64
	udpOversizeDrops atomic.Uint64
	inboundRateDrops atomic.Uint64
	voiceQueueDrops  atomic.Uint64
	ctrlQueueDrops   atomic.Uint64
	udpSendErrs      atomic.Uint64
	lastMuteCheck    atomic.Int64 // unix for silent-mute debug

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	started  atomic.Bool
}

// hybridPeer is per-remote-peer control + voice state.
type hybridPeer struct {
	id        string
	tcpAddr   string
	voiceAddr string

	mu     sync.Mutex
	conn   net.Conn
	ctrlQ  *dropOldestQueue[meshCtrlJob]
	voiceQ *dropOldestQueue[voiceJob]
	lastHB time.Time
	authed bool
	stop   chan struct{} // closed to stop writer/drainer for this conn gen

	// inbound rate (H-19)
	rateMu       sync.Mutex
	rateWindow   time.Time
	rateCount    int
	voiceTxCount atomic.Uint64
	voiceRxCount atomic.Uint64
	// lastVoiceUnixNano: last successful voice tx or rx (mute watch Issue 15)
	lastVoiceUnixNano atomic.Int64
}

type voiceJob struct {
	packet []byte // exclusive copy of full mesh frame
}

// NewHybridMesh constructs a HybridMesh (not started).
func NewHybridMesh(cfg HybridMeshConfig) (*HybridMesh, error) {
	if strings.TrimSpace(cfg.NodeID) == "" {
		return nil, fmt.Errorf("afv hybrid mesh: empty NodeID")
	}
	if strings.TrimSpace(cfg.ListenTCP) == "" || strings.TrimSpace(cfg.ListenVoice) == "" {
		return nil, fmt.Errorf("afv hybrid mesh: ListenTCP and ListenVoice required")
	}
	if strings.TrimSpace(cfg.PSK) == "" {
		return nil, fmt.Errorf("afv hybrid mesh: PSK required")
	}
	if len(cfg.Peers) == 0 {
		return nil, fmt.Errorf("afv hybrid mesh: at least one peer required")
	}
	if len(cfg.Peers) > 4 {
		return nil, fmt.Errorf("afv hybrid mesh: max 4 peers")
	}
	pg := cfg.PeerDeathAfter
	if pg <= 0 {
		pg = defaultPeerDeathGrace
	}
	hb := cfg.HeartbeatInterval
	if hb <= 0 {
		hb = defaultMeshHeartbeat
	}
	cd := cfg.CtrlQueueDepth
	if cd <= 0 {
		cd = meshControlQueueDepth
	}
	vd := cfg.VoiceQueueDepth
	if vd <= 0 {
		vd = meshVoiceQueueDepth
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	peerCfg := make(map[string]ClusterPeer, len(cfg.Peers))
	for _, p := range cfg.Peers {
		if p.ID == "" || p.ID == cfg.NodeID {
			return nil, fmt.Errorf("afv hybrid mesh: invalid peer id %q", p.ID)
		}
		if _, ok := peerCfg[p.ID]; ok {
			return nil, fmt.Errorf("afv hybrid mesh: duplicate peer %q", p.ID)
		}
		peerCfg[p.ID] = p
	}
	return &HybridMesh{
		cfg:            cfg,
		peerDeathGrace: pg,
		hbInterval:     hb,
		ctrlDepth:      cd,
		voiceDepth:     vd,
		logger:         log,
		peerCfg:        peerCfg,
		peerInterest:   make(map[string]map[FreqCell]struct{}),
		peers:          make(map[string]*hybridPeer),
		allowlist:      make(map[string]string),
		sendVoice:      make(map[string]*net.UDPAddr),
		stopCh:         make(chan struct{}),
	}, nil
}

// hybridConfigFrom builds HybridMeshConfig from AFV Config (bootstrap).
func hybridConfigFrom(cfg *Config) (HybridMeshConfig, error) {
	peers, err := parseClusterPeers(cfg.ClusterPeers)
	if err != nil {
		return HybridMeshConfig{}, err
	}
	return HybridMeshConfig{
		NodeID:      cfg.ClusterNodeID,
		ListenTCP:   cfg.ClusterListen,
		ListenVoice: cfg.ClusterVoiceListen,
		Peers:       peers,
		PSK:         cfg.ClusterPSK,
	}, nil
}

func (m *HybridMesh) NodeID() string {
	if m == nil {
		return ""
	}
	return m.cfg.NodeID
}

func (m *HybridMesh) OnAudioRelay(fn func(fromNode string, r AudioRelay)) {
	m.mu.Lock()
	m.onAudio = fn
	m.mu.Unlock()
}

func (m *HybridMesh) OnPeerDead(fn func(nodeID string)) {
	m.mu.Lock()
	m.onPeerDead = fn
	m.mu.Unlock()
}

func (m *HybridMesh) OnDirectory(fn MeshDirectoryHandler) {
	m.mu.Lock()
	m.onDir = fn
	m.mu.Unlock()
}

func (m *HybridMesh) SetSnapshotProvider(fn func() []MeshSessionBlock) {
	m.mu.Lock()
	m.snapFn = fn
	m.mu.Unlock()
}

func (m *HybridMesh) SetInterestProvider(fn func() []InterestEntry) {
	m.mu.Lock()
	m.interestFn = fn
	m.mu.Unlock()
}

// PeerWants reports whether peer advertised Interest for freq/cell (inbound TCP only).
func (m *HybridMesh) PeerWants(peerID string, freqHz uint32, cell geo.CellKey) bool {
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

// InterestedPeers returns peer IDs that want any of the given keys (authed only).
func (m *HybridMesh) InterestedPeers(keys []FreqCell) []string {
	if m == nil {
		return nil
	}
	// Snapshot under m.mu only — never nest p.mu.
	type snap struct {
		id  string
		p   *hybridPeer
		set map[FreqCell]struct{}
	}
	m.mu.RLock()
	snaps := make([]snap, 0, len(m.peerInterest))
	for pid, set := range m.peerInterest {
		if len(set) == 0 {
			continue
		}
		p := m.peers[pid]
		if p == nil {
			continue
		}
		// shallow ref to set (read-only after unlock for membership checks)
		snaps = append(snaps, snap{id: pid, p: p, set: set})
	}
	m.mu.RUnlock()

	var out []string
	for _, s := range snaps {
		s.p.mu.Lock()
		authed := s.p.authed
		s.p.mu.Unlock()
		if !authed {
			continue
		}
		for _, k := range keys {
			if _, ok := s.set[k]; ok {
				out = append(out, s.id)
				break
			}
		}
	}
	return out
}

// Start binds TCP+UDP, resolves allowlist, accept/dial, heartbeat.
// Single Start/Stop lifecycle — not safe to Start again after Stop.
func (m *HybridMesh) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("afv hybrid mesh: nil")
	}
	if !m.started.CompareAndSwap(false, true) {
		return nil // already started (or concurrent Start lost the race after first)
	}
	if err := m.resolveVoiceAllowlist(); err != nil {
		m.started.Store(false)
		return err
	}

	ln, err := net.Listen("tcp", m.cfg.ListenTCP)
	if err != nil {
		m.started.Store(false)
		return fmt.Errorf("afv hybrid mesh tcp listen: %w", err)
	}
	m.ln = ln
	m.logger.Info("AFV hybrid mesh TCP control listening", "addr", ln.Addr().String())

	pc, err := net.ListenPacket("udp", m.cfg.ListenVoice)
	if err != nil {
		_ = ln.Close()
		m.started.Store(false)
		return fmt.Errorf("afv hybrid mesh udp voice listen: %w", err)
	}
	m.voicePC = pc
	m.logger.Info("AFV hybrid mesh UDP voice listening", "addr", pc.LocalAddr().String())

	// Pre-create peer shells (queues re-init on Hello)
	for id, p := range m.peerCfg {
		m.mu.Lock()
		m.peers[id] = &hybridPeer{
			id:        id,
			tcpAddr:   p.Addr,
			voiceAddr: p.VoiceAddr,
			ctrlQ:     newDropOldestQueue[meshCtrlJob](m.ctrlDepth),
			voiceQ:    newDropOldestQueue[voiceJob](m.voiceDepth),
			stop:      make(chan struct{}),
		}
		m.peerInterest[id] = make(map[FreqCell]struct{})
		m.mu.Unlock()
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.acceptLoop(ctx)
	}()

	for id, p := range m.peerCfg {
		if id <= m.cfg.NodeID {
			continue // dial only if peer ID > self (H-9)
		}
		peer := p
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.dialLoop(ctx, peer)
		}()
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.heartbeatLoop(ctx)
	}()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.voiceReadLoop(ctx)
	}()

	// Per-peer voice drainers
	for id := range m.peerCfg {
		pid := id
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.voiceDrainLoop(ctx, pid)
		}()
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.muteWatchLoop(ctx)
	}()

	return nil
}

// Stop closes listeners/conns and waits for workers.
func (m *HybridMesh) Stop() error {
	if m == nil {
		return nil
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
		if m.ln != nil {
			_ = m.ln.Close()
		}
		if m.voicePC != nil {
			_ = m.voicePC.Close()
		}
		m.mu.Lock()
		for _, p := range m.peers {
			p.mu.Lock()
			if p.conn != nil {
				_ = p.conn.Close()
			}
			select {
			case <-p.stop:
			default:
				close(p.stop)
			}
			p.authed = false
			p.mu.Unlock()
		}
		m.mu.Unlock()
		m.started.Store(false)
	})
	m.wg.Wait()
	return nil
}

// PublishTrxSnapshot encodes and enqueues control to authed peers.
func (m *HybridMesh) PublishTrxSnapshot() {
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
		OriginNodeID: m.cfg.NodeID,
		Sessions:     sessions,
	})
	m.broadcastCtrl(MeshTypeTrxSnapshot, payload)
}

// PublishTrxDelta enqueues a directory delta.
func (m *HybridMesh) PublishTrxDelta(callsign string, isATC bool, trxs []Transceiver) {
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
		OriginNodeID: m.cfg.NodeID,
		Callsign:     callsign,
		IsATC:        isATC,
		Trxs:         meshTrxs,
	})
	m.broadcastCtrl(MeshTypeTrxDelta, payload)
}

// PublishSessionLeave enqueues leave.
func (m *HybridMesh) PublishSessionLeave(callsign string) {
	if m == nil {
		return
	}
	payload := EncodeSessionLeave(SessionLeavePayload{
		OriginNodeID: m.cfg.NodeID,
		Callsign:     callsign,
	})
	m.broadcastCtrl(MeshTypeSessionLeave, payload)
}

// PublishInterest encodes + enqueues control only (does not mutate local peerInterest).
func (m *HybridMesh) PublishInterest(entries []InterestEntry) {
	if m == nil {
		return
	}
	payload := EncodeInterest(InterestPayload{NodeID: m.cfg.NodeID, Entries: entries})
	m.broadcastCtrl(MeshTypeInterest, payload)
}

// EnqueueAudioRelay Interest-filters and enqueues UDP voice frames (MemoryMesh-compatible).
// Lock order: snapshot under m.mu, then p.mu alone (never nested).
func (m *HybridMesh) EnqueueAudioRelay(relay AudioRelay) {
	if m == nil {
		return
	}
	if relay.Audio != nil {
		relay.Audio = append([]byte(nil), relay.Audio...)
	}
	if relay.TxRadios != nil {
		relay.TxRadios = append([]RelayTxRadio(nil), relay.TxRadios...)
	}
	if relay.OriginNode == "" {
		relay.OriginNode = m.cfg.NodeID
	}
	if len(relay.TxRadios) > maxMeshVoiceTxRadios {
		return
	}

	// Snapshot peer pointers under m.mu only.
	m.mu.RLock()
	type snap struct {
		id string
		p  *hybridPeer
	}
	snaps := make([]snap, 0, len(m.peers))
	for id, p := range m.peers {
		snaps = append(snaps, snap{id: id, p: p})
	}
	m.mu.RUnlock()

	// Encode once; per-peer packet copy on enqueue.
	var packet []byte
	encoded := false

	for _, s := range snaps {
		s.p.mu.Lock()
		authed := s.p.authed
		q := s.p.voiceQ
		s.p.mu.Unlock()
		if !authed || q == nil {
			continue
		}
		want := false
		for _, tx := range relay.TxRadios {
			ck := geo.CellKey{
				ILat: geo.CellIndex(tx.LatDeg, geo.DefaultGridCellDeg),
				ILon: geo.CellIndex(tx.LonDeg, geo.DefaultGridCellDeg),
			}
			if m.PeerWants(s.id, tx.FreqHz, ck) {
				want = true
				break
			}
		}
		if !want {
			continue
		}
		if !encoded {
			payload, err := EncodeAudioRelay(relay)
			if err != nil {
				return
			}
			pkt, err := EncodeMeshFrameBytes(MeshTypeAudioRelay, payload)
			if err != nil || len(pkt) > maxMeshVoiceDatagram {
				return
			}
			packet = pkt
			encoded = true
		}
		job := voiceJob{packet: append([]byte(nil), packet...)}
		if q.Enqueue(job) {
			m.voiceQueueDrops.Add(1)
		}
	}
}

func (m *HybridMesh) broadcastCtrl(typ byte, payload []byte) {
	m.mu.RLock()
	peers := make([]*hybridPeer, 0, len(m.peers))
	for _, p := range m.peers {
		peers = append(peers, p)
	}
	m.mu.RUnlock()
	for _, p := range peers {
		p.mu.Lock()
		authed := p.authed
		q := p.ctrlQ
		p.mu.Unlock()
		if !authed || q == nil {
			continue
		}
		job := meshCtrlJob{typ: typ, payload: append([]byte(nil), payload...)}
		if q.Enqueue(job) {
			m.ctrlQueueDrops.Add(1)
			m.logger.Debug("AFV hybrid mesh control drop", "peer", p.id, "type", typ)
		}
	}
}

// PeerAuthedForTest reports whether peer is control-authed (tests).
func (m *HybridMesh) PeerAuthedForTest(peerID string) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.authed
}

// WaitPeerAuthed waits until peer is authed or timeout.
func (m *HybridMesh) WaitPeerAuthed(peerID string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if m.PeerAuthedForTest(peerID) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ForcePeerDeathForTest closes the peer TCP conn to trigger death path (tests).
func (m *HybridMesh) ForcePeerDeathForTest(peerID string) {
	if m == nil {
		return
	}
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	c := p.conn
	p.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// UDPAllowDrops / InboundRateDrops / VoiceQueueDrops expose counters for tests.
func (m *HybridMesh) UDPAllowDrops() uint64    { return m.udpAllowDrops.Load() }
func (m *HybridMesh) UDPOversizeDrops() uint64 { return m.udpOversizeDrops.Load() }
func (m *HybridMesh) InboundRateDrops() uint64 { return m.inboundRateDrops.Load() }
func (m *HybridMesh) VoiceQueueDrops() uint64  { return m.voiceQueueDrops.Load() }
func (m *HybridMesh) CtrlQueueDrops() uint64   { return m.ctrlQueueDrops.Load() }
func (m *HybridMesh) VoiceTx() uint64          { return m.voiceTx.Load() }
func (m *HybridMesh) VoiceRx() uint64          { return m.voiceRx.Load() }

// CurrentConnForTest returns the current TCP conn for peer (tests / dual-conn).
func (m *HybridMesh) CurrentConnForTest(peerID string) net.Conn {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn
}

// ApplyInterestDirect is test helper for Interest maps without TCP.
func (m *HybridMesh) ApplyInterestDirect(fromPeer string, entries []InterestEntry) {
	set := make(map[FreqCell]struct{}, len(entries))
	for _, e := range entries {
		set[FreqCell{FreqHz: e.FreqHz, Cell: geo.CellKey{ILat: e.ILat, ILon: e.ILon}}] = struct{}{}
	}
	m.mu.Lock()
	m.peerInterest[fromPeer] = set
	m.mu.Unlock()
}

// ForceEnqueueCtrlForTest fills control queue (drop-oldest tests).
func (m *HybridMesh) ForceEnqueueCtrlForTest(peerID string, n int) {
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	q := p.ctrlQ
	p.mu.Unlock()
	if q == nil {
		return
	}
	for i := 0; i < n; i++ {
		job := meshCtrlJob{typ: MeshTypeHeartbeat, payload: EncodeHeartbeatPayload(uint64(i))}
		if q.Enqueue(job) {
			m.ctrlQueueDrops.Add(1)
		}
	}
}

// ForceEnqueueVoiceForTest fills voice queue (drop-oldest tests).
func (m *HybridMesh) ForceEnqueueVoiceForTest(peerID string, n int) {
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	q := p.voiceQ
	p.mu.Unlock()
	if q == nil {
		return
	}
	pkt := make([]byte, 32)
	for i := 0; i < n; i++ {
		job := voiceJob{packet: append([]byte(nil), pkt...)}
		if q.Enqueue(job) {
			m.voiceQueueDrops.Add(1)
		}
	}
}

// PeerCtrlQueueLen / PeerVoiceQueueLen for tests.
func (m *HybridMesh) PeerCtrlQueueLen(peerID string) int {
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return 0
	}
	p.mu.Lock()
	q := p.ctrlQ
	p.mu.Unlock()
	if q == nil {
		return 0
	}
	return q.Len()
}

func (m *HybridMesh) PeerVoiceQueueLen(peerID string) int {
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return 0
	}
	p.mu.Lock()
	q := p.voiceQ
	p.mu.Unlock()
	if q == nil {
		return 0
	}
	return q.Len()
}

// ListenTCPAddr / ListenVoiceAddr return bound addresses after Start (tests, :0).
func (m *HybridMesh) ListenTCPAddr() string {
	if m == nil || m.ln == nil {
		return m.cfg.ListenTCP
	}
	return m.ln.Addr().String()
}

func (m *HybridMesh) ListenVoiceAddr() string {
	if m == nil || m.voicePC == nil {
		return m.cfg.ListenVoice
	}
	return m.voicePC.LocalAddr().String()
}

var _ Mesh = (*HybridMesh)(nil)
