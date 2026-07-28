package afv

// HybridMesh TCP control plane: listen/dial, Hello, HB, directory, Interest, peer death.
// AudioRelay (type 20) is forbidden on TCP (H-2).

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/renorris/openfsd/internal/geo"
)

func (m *HybridMesh) acceptLoop(ctx context.Context) {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			default:
			}
			// Permanent close of listener → exit; transient errors → backoff + continue.
			if errors.Is(err, net.ErrClosed) {
				return
			}
			m.logger.Debug("AFV hybrid mesh accept error", "err", err)
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.serveConn(conn)
		}()
	}
}

func (m *HybridMesh) dialLoop(ctx context.Context, peer ClusterPeer) {
	backoff := 200 * time.Millisecond
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		// Skip dial if already authed with live conn.
		m.mu.RLock()
		p := m.peers[peer.ID]
		m.mu.RUnlock()
		if p != nil {
			p.mu.Lock()
			live := p.authed && p.conn != nil
			p.mu.Unlock()
			if live {
				select {
				case <-m.stopCh:
					return
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
				continue
			}
		}
		d := net.Dialer{Timeout: 5 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", peer.Addr)
		if err != nil {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 200 * time.Millisecond
		m.serveConn(conn)
		// reconnect after disconnect
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (m *HybridMesh) serveConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Send Hello immediately
	helloPay := EncodeHelloPayload(HelloPayload{NodeID: m.cfg.NodeID, PSK: m.cfg.PSK})
	if err := EncodeMeshFrame(conn, MeshTypeHello, helloPay); err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	fr, err := DecodeMeshFrame(conn)
	if err != nil || fr.Type != MeshTypeHello {
		m.logger.Warn("AFV hybrid mesh non-Hello first frame or read error", "err", err)
		return
	}
	hp, err := DecodeHelloPayload(fr.Payload)
	if err != nil {
		return
	}
	if err := VerifyHelloPSK(m.cfg.PSK, hp.PSK); err != nil {
		m.logger.Warn("AFV hybrid mesh hello auth failed", "peer", hp.NodeID)
		return
	}
	peerID := strings.TrimSpace(hp.NodeID)
	if peerID == "" || peerID == m.cfg.NodeID {
		m.logger.Warn("AFV hybrid mesh reject self or empty peer")
		return
	}
	if _, ok := m.peerCfg[peerID]; !ok {
		m.logger.Warn("AFV hybrid mesh reject unknown peer", "peer", peerID)
		return
	}
	// H-15: log TCP RemoteAddr host ≠ configured peer TCP host as warn only
	if expected, ok := m.peerCfg[peerID]; ok && expected.Addr != "" {
		if ra := conn.RemoteAddr(); ra != nil {
			host, _, _ := net.SplitHostPort(ra.String())
			expHost, _, _ := net.SplitHostPort(expected.Addr)
			if host != "" && expHost != "" && host != expHost && expHost != "0.0.0.0" {
				m.logger.Warn("AFV hybrid mesh Hello RemoteAddr host differs from config",
					"peer", peerID, "remote", host, "configured", expHost)
			}
		}
	}
	_ = conn.SetReadDeadline(time.Time{})

	// Install peer (H-9 dual-conn replace); capture this generation's stop+queues.
	// Soft replace does not run peerDeath / OnPeerDead.
	p, stop, ctrlQ := m.installPeerConn(peerID, conn)
	if p == nil {
		return
	}

	// Per-peer TCP writer for this generation only (bound to ctrlQ + stop)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.peerCtrlWriter(p, conn, ctrlQ, stop)
	}()

	// Post-Hello sequence: Snapshot then Interest (M-16) on TCP only
	m.sendPostHello(ctrlQ)

	// Read loop
	for {
		select {
		case <-m.stopCh:
			return
		case <-stop:
			// Superseded by dual-conn replace or death of this generation — exit quietly.
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(m.peerDeathGrace + 5*time.Second))
		fr, err := DecodeMeshFrame(conn)
		if err != nil {
			// Generation-scoped: only tear down if this conn is still current.
			m.peerDeath(peerID, "read", conn)
			return
		}
		// Generation gate (same as lastHB): superseded dual-conn readers must not
		// apply Interest/directory or peerDeath after a successful late decode.
		p.mu.Lock()
		stillMine := p.conn == conn
		if stillMine {
			p.lastHB = time.Now() // H-8: every post-Hello control frame
		}
		p.mu.Unlock()
		if !stillMine {
			return
		}

		if !m.handleCtrlFrame(peerID, fr, conn) {
			return // closed (type 20, bad, unexpected Hello)
		}
	}
}

// installPeerConn replaces any existing conn for peerID (H-9).
// Soft replace: closes previous generation stop+conn without OnPeerDead / Interest wipe.
// Returns the peer, this generation's stop channel, and ctrl queue.
func (m *HybridMesh) installPeerConn(peerID string, conn net.Conn) (*hybridPeer, chan struct{}, *dropOldestQueue[meshCtrlJob]) {
	m.mu.RLock()
	p, ok := m.peers[peerID]
	m.mu.RUnlock()
	if !ok {
		return nil, nil, nil
	}

	p.mu.Lock()
	// Close previous live generation (soft replace — no peerDeath)
	if p.conn != nil && p.conn != conn {
		select {
		case <-p.stop:
		default:
			close(p.stop)
		}
		_ = p.conn.Close()
	}
	// Reset queues + stop channel for new generation
	ctrlQ := newDropOldestQueue[meshCtrlJob](m.ctrlDepth)
	voiceQ := newDropOldestQueue[voiceJob](m.voiceDepth)
	stop := make(chan struct{})
	p.ctrlQ = ctrlQ
	p.voiceQ = voiceQ
	p.stop = stop
	p.conn = conn
	p.authed = true
	p.lastHB = time.Now()
	p.mu.Unlock()

	m.logger.Info("AFV hybrid mesh peer authenticated", "peer", peerID)
	return p, stop, ctrlQ
}

func (m *HybridMesh) sendPostHello(ctrlQ *dropOldestQueue[meshCtrlJob]) {
	if ctrlQ == nil {
		return
	}
	// Snapshot
	m.mu.RLock()
	snapFn := m.snapFn
	m.mu.RUnlock()
	var sessions []MeshSessionBlock
	if snapFn != nil {
		sessions = snapFn()
	}
	snap := EncodeTrxSnapshot(TrxSnapshotPayload{
		OriginNodeID: m.cfg.NodeID,
		Sessions:     sessions,
	})
	_ = ctrlQ.Enqueue(meshCtrlJob{typ: MeshTypeTrxSnapshot, payload: snap})
	// Interest
	m.mu.RLock()
	ifn := m.interestFn
	m.mu.RUnlock()
	var entries []InterestEntry
	if ifn != nil {
		entries = ifn()
	}
	ipay := EncodeInterest(InterestPayload{NodeID: m.cfg.NodeID, Entries: entries})
	_ = ctrlQ.Enqueue(meshCtrlJob{typ: MeshTypeInterest, payload: ipay})
}

func (m *HybridMesh) peerCtrlWriter(p *hybridPeer, conn net.Conn, q *dropOldestQueue[meshCtrlJob], stop chan struct{}) {
	if q == nil {
		return
	}
	for {
		select {
		case <-m.stopCh:
			return
		case <-stop:
			return
		case job, ok := <-q.Chan():
			if !ok {
				return
			}
			if err := EncodeMeshFrame(conn, job.typ, job.payload); err != nil {
				m.peerDeath(p.id, "write", conn)
				return
			}
		}
	}
}

// handleCtrlFrame returns false if the peer should be closed.
// deadConn is the TCP conn of this reader generation (for generation-scoped death/apply).
// Superseded readers (p.conn != deadConn) are no-ops for apply and death.
func (m *HybridMesh) handleCtrlFrame(peerID string, fr MeshFrame, deadConn net.Conn) bool {
	// Defense in depth: re-check generation before any side effects.
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return false
	}
	p.mu.Lock()
	stillMine := p.conn == deadConn
	p.mu.Unlock()
	if !stillMine {
		return true // no apply / no death; caller may already be exiting
	}

	switch fr.Type {
	case MeshTypeHello:
		m.logger.Warn("AFV hybrid mesh unexpected Hello after auth", "peer", peerID)
		m.peerDeath(peerID, "unexpected Hello", deadConn)
		return false
	case MeshTypeHeartbeat:
		return true
	case MeshTypeTrxSnapshot:
		pl, err := DecodeTrxSnapshot(fr.Payload)
		if err != nil {
			m.peerDeath(peerID, "bad TrxSnapshot", deadConn)
			return false
		}
		if pl.OriginNodeID != peerID {
			m.peerDeath(peerID, "snapshot origin mismatch", deadConn)
			return false
		}
		m.mu.RLock()
		dir := m.onDir
		m.mu.RUnlock()
		if dir != nil {
			dir.ApplySnapshot(pl.OriginNodeID, meshSessionsToRemote(pl.Sessions))
		}
		return true
	case MeshTypeTrxDelta:
		pl, err := DecodeTrxDelta(fr.Payload)
		if err != nil {
			m.peerDeath(peerID, "bad TrxDelta", deadConn)
			return false
		}
		if pl.OriginNodeID != peerID {
			m.peerDeath(peerID, "delta origin mismatch", deadConn)
			return false
		}
		m.mu.RLock()
		dir := m.onDir
		m.mu.RUnlock()
		if dir != nil {
			dir.ApplyDelta(pl.OriginNodeID, pl.Callsign, pl.IsATC, meshTrxToLocal(pl.Trxs))
		}
		return true
	case MeshTypeSessionLeave:
		pl, err := DecodeSessionLeave(fr.Payload)
		if err != nil {
			m.peerDeath(peerID, "bad SessionLeave", deadConn)
			return false
		}
		if pl.OriginNodeID != peerID {
			m.peerDeath(peerID, "leave origin mismatch", deadConn)
			return false
		}
		m.mu.RLock()
		dir := m.onDir
		m.mu.RUnlock()
		if dir != nil {
			dir.ApplyLeave(pl.OriginNodeID, pl.Callsign)
		}
		return true
	case MeshTypeAudioRelay:
		// H-2: type 20 forbidden on TCP
		m.logger.Warn("AFV hybrid mesh type 20 AudioRelay on TCP — closing peer", "peer", peerID)
		m.peerDeath(peerID, "audio on TCP", deadConn)
		return false
	case MeshTypeInterest:
		// H-14: full replace for origin
		pl, err := DecodeInterest(fr.Payload)
		if err != nil {
			m.peerDeath(peerID, "bad Interest", deadConn)
			return false
		}
		if pl.NodeID != peerID {
			m.peerDeath(peerID, "interest node mismatch", deadConn)
			return false
		}
		set := make(map[FreqCell]struct{}, len(pl.Entries))
		for _, e := range pl.Entries {
			set[FreqCell{FreqHz: e.FreqHz, Cell: geo.CellKey{ILat: e.ILat, ILon: e.ILon}}] = struct{}{}
		}
		m.mu.Lock()
		m.peerInterest[peerID] = set
		m.mu.Unlock()
		return true
	default:
		m.logger.Warn("AFV hybrid mesh unknown control type", "peer", peerID, "type", fr.Type)
		m.peerDeath(peerID, fmt.Sprintf("unknown type %d", fr.Type), deadConn)
		return false
	}
}

// peerDeath is generation-scoped death procedure (H-8 / H-9).
// Only tears down if deadConn is still the peer's current conn (or deadConn is nil
// and the peer is authed — used for heartbeat timeout with captured conn).
// Lock order: p.mu alone, then m.mu alone — never nested.
func (m *HybridMesh) peerDeath(peerID, reason string, deadConn net.Conn) {
	m.mu.RLock()
	p := m.peers[peerID]
	m.mu.RUnlock()
	if p == nil {
		return
	}

	p.mu.Lock()
	// Generation guard: stale reader/writer from dual-conn replace must not kill successor.
	if deadConn != nil && p.conn != deadConn {
		p.mu.Unlock()
		return
	}
	if deadConn == nil && (!p.authed || p.conn == nil) {
		p.mu.Unlock()
		return
	}
	wasAuthed := p.authed
	p.authed = false
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	// re-init empty queues
	p.ctrlQ = newDropOldestQueue[meshCtrlJob](m.ctrlDepth)
	p.voiceQ = newDropOldestQueue[voiceJob](m.voiceDepth)
	p.mu.Unlock()

	// clear Interest + load callback under m.mu only (no p.mu)
	m.mu.Lock()
	m.peerInterest[peerID] = make(map[FreqCell]struct{})
	cb := m.onPeerDead
	m.mu.Unlock()

	if wasAuthed {
		m.logger.Warn("AFV hybrid mesh peer dead", "peer", peerID, "reason", reason)
		if cb != nil {
			cb(peerID)
		}
	}
}

func (m *HybridMesh) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(m.hbInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			ms := uint64(now.UnixMilli())
			pay := EncodeHeartbeatPayload(ms)

			// Snapshot peer pointers under m.mu only — never nest p.mu.
			m.mu.RLock()
			peers := make([]*hybridPeer, 0, len(m.peers))
			for _, p := range m.peers {
				peers = append(peers, p)
			}
			m.mu.RUnlock()

			for _, p := range peers {
				p.mu.Lock()
				authed := p.authed
				last := p.lastHB
				conn := p.conn
				q := p.ctrlQ
				p.mu.Unlock()
				if !authed {
					continue
				}
				if !last.IsZero() && now.Sub(last) > m.peerDeathGrace {
					m.peerDeath(p.id, "heartbeat timeout", conn)
					continue
				}
				if q != nil {
					_ = q.Enqueue(meshCtrlJob{typ: MeshTypeHeartbeat, payload: pay})
				}
			}
		}
	}
}
