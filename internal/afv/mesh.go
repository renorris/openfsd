package afv

// AFV multi-node mesh (PR-10 / KD-17).
//
// Ops: sticky LB required — each node mints unique ChannelTag + AEAD keys and
// advertises its own AFV_UDP_ADVERTISE_IPV4. Clients must use the same node for
// REST session create and UDP for the life of that session. Mesh does not
// replace sticky affinity. AEAD keys never leave the home node; cross-node
// audio is re-encrypted at the listener's home with local ClientRxKey only.
//
// Production: AFV_CLUSTER_ENABLED=true without TCP mesh in this binary fails
// closed at startup (MemoryMesh is tests-only via SetMesh).

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/renorris/openfsd/internal/geo"
	"github.com/renorris/openfsd/pkg/afvprotocol"
)

// Mesh is the AFV inter-node fabric. Implementations: MemoryMesh (tests);
// TCPMesh is PR-10b / optional.
//
// All publish methods must be non-blocking w.r.t. the UDP hot path (enqueue or
// drop). Callers must not hold registry locks across Publish*/Enqueue*.
type Mesh interface {
	Start(ctx context.Context) error
	Stop() error
	NodeID() string

	// Directory publish (local → peers).
	PublishTrxSnapshot()
	PublishTrxDelta(callsign string, isATC bool, trxs []Transceiver)
	PublishSessionLeave(callsign string)

	// Voice relay (local → interested peers only).
	// Mesh must not retain caller's Audio buffer after EnqueueAudioRelay returns.
	EnqueueAudioRelay(relay AudioRelay)

	// Interest (local RX need → peers).
	PublishInterest(entries []InterestEntry)

	// PeerInterest reports whether peer wants this freq/cell (for TX fan-out).
	PeerWants(peerID string, freqHz uint32, cell geo.CellKey) bool
	// InterestedPeers returns peer IDs that want any of the given keys.
	InterestedPeers(keys []FreqCell) []string

	// Callbacks registered before Start.
	OnAudioRelay(fn func(fromNode string, r AudioRelay))
	OnPeerDead(fn func(nodeID string))
	// OnDirectory applies inbound directory frames.
	OnDirectory(fn MeshDirectoryHandler)
	// SetSnapshotProvider supplies local session blocks for TrxSnapshot.
	SetSnapshotProvider(fn func() []MeshSessionBlock)
}

// MeshDirectoryHandler receives remote directory updates from peers.
type MeshDirectoryHandler interface {
	ApplySnapshot(origin string, sessions []RemoteSession)
	ApplyDelta(origin, callsign string, isATC bool, trxs []Transceiver)
	ApplyLeave(origin, callsign string)
	RemoveNode(origin string)
}

// FreqCell is a (frequency, cell) interest key.
type FreqCell struct {
	FreqHz uint32
	Cell   geo.CellKey
}

// InterestEntry is one Interest wire entry.
type InterestEntry struct {
	FreqHz uint32
	ILat   int32
	ILon   int32
}

// AudioRelay is mesh type 20 — opaque Opus + TX geometry. No AEAD keys.
type AudioRelay struct {
	OriginNode      string
	Callsign        string
	SequenceCounter uint32
	LastPacket      bool
	IsATC           bool // session-level TX class; required for ClassifyRange on remote
	IsXC            bool
	Audio           []byte
	TxRadios        []RelayTxRadio
}

// RelayTxRadio is one TX radio on an AudioRelay.
type RelayTxRadio struct {
	TxID    uint16
	FreqHz  uint32
	LatDeg  float64
	LonDeg  float64
	HeightM float64
}

// MeshConfig is shared construction config for MemoryMesh / future TCPMesh.
type MeshConfig struct {
	NodeID  string
	PSK     string
	PeerIDs []string // may include self; remotes are others
	Logger  *slog.Logger
}

// interest constants (M-5 / M-6)
const (
	interestMinInterval   = 500 * time.Millisecond // ≤ 2 Hz
	defaultPeerDeathGrace = 15 * time.Second
)

// interestMaxEntries is the Interest set cap (M-5). Var so tests can lower it.
var interestMaxEntries = 4096

// --- Cluster config validation (M-11) ---

// ClusterPeer is one static remote peer (id + host:port).
type ClusterPeer struct {
	ID   string
	Addr string
}

// ValidateCluster fails closed when cluster is enabled with incomplete config.
func (c *Config) ValidateCluster() error {
	if c == nil || !c.ClusterEnabled {
		return nil
	}
	if strings.TrimSpace(c.ClusterNodeID) == "" {
		return fmt.Errorf("AFV_CLUSTER_NODE_ID required when AFV_CLUSTER_ENABLED=true")
	}
	if strings.TrimSpace(c.ClusterListen) == "" {
		return fmt.Errorf("AFV_CLUSTER_LISTEN required when AFV_CLUSTER_ENABLED=true")
	}
	if strings.TrimSpace(c.ClusterPSK) == "" {
		return fmt.Errorf("AFV_CLUSTER_PSK required when AFV_CLUSTER_ENABLED=true")
	}
	peers, err := parseClusterPeers(c.ClusterPeers)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		return fmt.Errorf("AFV_CLUSTER_PEERS required when AFV_CLUSTER_ENABLED=true")
	}
	if len(peers) > 4 {
		return fmt.Errorf("AFV_CLUSTER_PEERS: max 4 remote peers, got %d", len(peers))
	}
	self := strings.TrimSpace(c.ClusterNodeID)
	seen := make(map[string]struct{}, len(peers))
	for _, p := range peers {
		if p.ID == self {
			return fmt.Errorf("AFV_CLUSTER_PEERS: must not include self node id %q", self)
		}
		if p.ID == "" || p.Addr == "" {
			return fmt.Errorf("AFV_CLUSTER_PEERS: empty id or addr")
		}
		if _, ok := seen[p.ID]; ok {
			return fmt.Errorf("AFV_CLUSTER_PEERS: duplicate peer id %q", p.ID)
		}
		seen[p.ID] = struct{}{}
	}
	return nil
}

// parseClusterPeers parses "id=host:port,id2=host:port2".
func parseClusterPeers(s string) ([]ClusterPeer, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]ClusterPeer, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq <= 0 || eq == len(p)-1 {
			return nil, fmt.Errorf("AFV_CLUSTER_PEERS: invalid entry %q (want id=host:port)", p)
		}
		id := strings.TrimSpace(p[:eq])
		addr := strings.TrimSpace(p[eq+1:])
		if id == "" || addr == "" {
			return nil, fmt.Errorf("AFV_CLUSTER_PEERS: invalid entry %q", p)
		}
		out = append(out, ClusterPeer{ID: id, Addr: addr})
	}
	return out, nil
}

// errClusterTCPNotBuilt is returned when ENABLED=true but TCP mesh is not available.
var errClusterTCPNotBuilt = fmt.Errorf(
	"AFV_CLUSTER_ENABLED=true but AFV mesh TCP is not built in this binary (use tests with SetMesh(MemoryMesh) or enable PR-10b TCP)")

// --- Server mesh hooks ---

// SetMesh injects a Mesh (tests: MemoryMesh). Must be called before Run.
// Production NewDefault never installs MemoryMesh.
func (s *Server) SetMesh(m Mesh) {
	if s == nil {
		return
	}
	s.mesh = m
	if m != nil && s.remote == nil {
		s.remote = newRemoteDir()
	}
}

// Mesh returns the attached mesh (tests).
func (s *Server) Mesh() Mesh {
	if s == nil {
		return nil
	}
	return s.mesh
}

// RemoteDir returns the remote directory (tests).
func (s *Server) RemoteDir() *remoteDir {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *Server) markInterestDirty() {
	if s == nil {
		return
	}
	s.interestDirty.Store(true)
}

func (s *Server) meshPublishLeaves(leaves []string) {
	if s == nil || s.mesh == nil || len(leaves) == 0 {
		return
	}
	for _, cs := range leaves {
		s.mesh.PublishSessionLeave(cs)
	}
	s.markInterestDirty()
}

func (s *Server) meshPublishDelta(callsign string, isATC bool, trxs []Transceiver) {
	if s == nil || s.mesh == nil {
		return
	}
	s.mesh.PublishTrxDelta(callsign, isATC, trxs)
	s.markInterestDirty()
}

// registerMeshCallbacks wires inbound handlers before Start.
func (s *Server) registerMeshCallbacks() {
	if s == nil || s.mesh == nil {
		return
	}
	if s.remote == nil {
		s.remote = newRemoteDir()
	}
	s.mesh.OnAudioRelay(s.handleMeshAudioRelay)
	s.mesh.OnPeerDead(func(nodeID string) {
		if s.remote != nil {
			s.remote.RemoveNode(nodeID)
		}
		slog.Warn("AFV mesh peer dead", "peer", nodeID)
	})
	s.mesh.OnDirectory(s.remote)
	s.mesh.SetSnapshotProvider(func() []MeshSessionBlock {
		return s.reg.snapshotLocalSessionsForMesh()
	})
}

// runInterestLoop publishes Interest ≤ 2 Hz when dirty (M-5 / M-15 / test 20).
func (s *Server) runInterestLoop(ctx context.Context) {
	if s == nil || s.mesh == nil {
		return
	}
	ticker := time.NewTicker(interestMinInterval)
	defer ticker.Stop()
	s.publishInterestNow()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.interestDirty.Swap(false) {
				s.publishInterestNow()
			}
		}
	}
}

func (s *Server) publishInterestNow() {
	if s == nil || s.mesh == nil || s.reg == nil {
		return
	}
	entries := s.reg.buildInterestEntries(s.cfg)
	s.mesh.PublishInterest(entries)
}

// handleMeshAudioRelay routes inbound AudioRelay to local bound RX (M-8).
func (s *Server) handleMeshAudioRelay(fromNode string, r AudioRelay) {
	if s == nil || s.reg == nil {
		return
	}
	// isXC: primary synthetic TX only; never XC-again (PR-9 not implemented).
	_ = r.IsXC
	_ = fromNode

	s.udpMu.Lock()
	pc := s.udpConn
	s.udpMu.Unlock()
	if pc == nil {
		return // UDP not ready — drop silently
	}

	recipients := s.reg.routeSyntheticTX(r.Callsign, r.IsATC, r.TxRadios)
	for _, rec := range recipients {
		if rec.udp == nil || rec.sess == nil {
			continue
		}
		ar := afvprotocol.AudioRx{
			Callsign:        r.Callsign,
			SequenceCounter: r.SequenceCounter,
			Audio:           r.Audio,
			LastPacket:      r.LastPacket,
			Transceivers:    rec.rx,
		}
		ch, err := afvprotocol.ServerChannel(rec.tag, rec.rxKey[:], rec.txKey[:])
		if err != nil {
			continue
		}
		seq := rec.sess.nextTxSeq()
		pkt, err := ch.Encapsulate(seq, afvprotocol.DTONameAudioRx, ar.EncodeMsgpack(), nil)
		if err != nil {
			continue
		}
		addr, ok := rec.udp.(net.Addr)
		if !ok {
			continue
		}
		_, _ = pc.WriteTo(pkt, addr)
	}
}

// snapshotTXForMesh copies session IsATC + TX radio geometry for AT radios under RLock.
func (r *Registry) snapshotTXForMesh(sess *VoiceSession, atRadios []afvprotocol.TxTransceiver) (isATC bool, radios []RelayTxRadio) {
	if r == nil || sess == nil {
		return false, nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	isATC = sess.IsATC
	byID := make(map[uint16]Transceiver, len(sess.Transceivers))
	for _, t := range sess.Transceivers {
		byID[t.ID] = t
	}
	radios = make([]RelayTxRadio, 0, len(atRadios))
	for _, tr := range atRadios {
		t, ok := byID[tr.ID]
		if !ok {
			continue
		}
		radios = append(radios, RelayTxRadio{
			TxID:    t.ID,
			FreqHz:  t.Frequency,
			LatDeg:  t.LatDeg,
			LonDeg:  t.LonDeg,
			HeightM: t.HeightMslM,
		})
	}
	return isATC, radios
}

// snapshotLocalSessionsForMesh copies local sessions for TrxSnapshot (under RLock).
func (r *Registry) snapshotLocalSessionsForMesh() []MeshSessionBlock {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]MeshSessionBlock, 0, len(r.byTag))
	for _, sess := range r.byTag {
		if sess == nil {
			continue
		}
		trxs := make([]MeshTrx, 0, len(sess.Transceivers))
		for _, t := range sess.Transceivers {
			trxs = append(trxs, MeshTrx{
				ID: t.ID, FreqHz: t.Frequency,
				LatDeg: t.LatDeg, LonDeg: t.LonDeg, AltM: t.HeightMslM,
			})
		}
		out = append(out, MeshSessionBlock{
			Callsign:   sess.CallsignKey,
			ChannelTag: sess.ChannelTag,
			IsATC:      sess.IsATC,
			Trxs:       trxs,
		})
	}
	return out
}

var _ MeshDirectoryHandler = (*remoteDir)(nil)

// InterestDirtyForTest reports interest dirty flag (tests).
func (s *Server) InterestDirtyForTest() bool {
	if s == nil {
		return false
	}
	return s.interestDirty.Load()
}

// PublishInterestNowForTest forces interest recompute (tests).
func (s *Server) PublishInterestNowForTest() {
	s.publishInterestNow()
}

// MarkInterestDirtyForTest sets dirty (tests).
func (s *Server) MarkInterestDirtyForTest() {
	s.markInterestDirty()
}
