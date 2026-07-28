package afv

// HybridMesh UDP voice plane: AudioRelay (type 20) only.
// Allowlist H-16, maxMeshVoiceDatagram H-12, inbound rate H-19.

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// maxMeshVoiceDatagram is the only accepted UDP voice size (bytes). H-12.
// Do NOT use MaxMeshPayload (1 MiB) for UDP reads.
// Var so tests can lower the cap without exceeding OS UDP send limits.
var maxMeshVoiceDatagram = 16 << 10 // 16384

// maxMeshVoiceTxRadios caps TxRadios on mesh AudioRelay for voice path.
const maxMeshVoiceTxRadios = 64

// meshVoiceInboundRatePerSec soft cap per authed peer (datagrams/s). H-19.
const meshVoiceInboundRatePerSec = 5000

// resolveVoiceAllowlist resolves peer VoiceAddrs at Start (H-15 / H-16).
func (m *HybridMesh) resolveVoiceAllowlist() error {
	allow := make(map[string]string)
	send := make(map[string]*net.UDPAddr)

	for id, p := range m.peerCfg {
		host, portStr, err := net.SplitHostPort(p.VoiceAddr)
		if err != nil {
			return fmt.Errorf("afv hybrid mesh peer %q VoiceAddr: %w", id, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("afv hybrid mesh peer %q voice port invalid", id)
		}

		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			resolved, err := net.LookupIP(host)
			if err != nil || len(resolved) == 0 {
				return fmt.Errorf("afv hybrid mesh resolve peer %q voice host %q: %w", id, host, err)
			}
			ips = resolved
		}

		// Prefer IPv4 for send target
		var sendIP net.IP
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				sendIP = v4
				break
			}
		}
		if sendIP == nil {
			sendIP = ips[0]
		}
		send[id] = &net.UDPAddr{IP: sendIP, Port: port}

		for _, ip := range ips {
			for _, key := range canonicalUDPKeys(ip, port) {
				if other, ok := allow[key]; ok && other != id {
					return fmt.Errorf("afv hybrid mesh allowlist collision key %q peers %q and %q", key, other, id)
				}
				allow[key] = id
			}
		}
	}
	m.allowlist = allow
	m.sendVoice = send
	return nil
}

// canonicalUDPKeys returns H-16 dual keys for IPv4 / IPv4-mapped IPv6.
func canonicalUDPKeys(ip net.IP, port int) []string {
	var keys []string
	ip16 := ip.To16()
	if ip16 != nil {
		keys = append(keys, net.JoinHostPort(ip16.String(), strconv.Itoa(port)))
	}
	if v4 := ip.To4(); v4 != nil {
		keys = append(keys, net.JoinHostPort(v4.String(), strconv.Itoa(port)))
		// IPv4-mapped form explicitly
		mapped := net.IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, v4[0], v4[1], v4[2], v4[3]}
		keys = append(keys, net.JoinHostPort(mapped.String(), strconv.Itoa(port)))
	}
	return keys
}

func (m *HybridMesh) lookupAllowlist(src net.Addr) string {
	ua, ok := src.(*net.UDPAddr)
	if !ok {
		host, portStr, err := net.SplitHostPort(src.String())
		if err != nil {
			return ""
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return ""
		}
		port, _ := strconv.Atoi(portStr)
		ua = &net.UDPAddr{IP: ip, Port: port}
	}
	for _, key := range canonicalUDPKeys(ua.IP, ua.Port) {
		if id, ok := m.allowlist[key]; ok {
			return id
		}
	}
	// also try raw JoinHostPort of To16
	if ua.IP != nil {
		k := net.JoinHostPort(ua.IP.String(), strconv.Itoa(ua.Port))
		if id, ok := m.allowlist[k]; ok {
			return id
		}
	}
	return ""
}

func (m *HybridMesh) voiceReadLoop(ctx context.Context) {
	buf := make([]byte, maxMeshVoiceDatagram+1)
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		if m.voicePC == nil {
			return
		}
		_ = m.voicePC.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, src, err := m.voicePC.ReadFrom(buf)
		if err != nil {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			default:
				// timeout or transient
				continue
			}
		}
		if n > maxMeshVoiceDatagram {
			m.udpOversizeDrops.Add(1)
			continue
		}
		peerID := m.lookupAllowlist(src)
		if peerID == "" {
			m.udpAllowDrops.Add(1)
			continue
		}
		m.mu.RLock()
		p := m.peers[peerID]
		m.mu.RUnlock()
		if p == nil {
			m.udpAllowDrops.Add(1)
			continue
		}
		p.mu.Lock()
		authed := p.authed
		p.mu.Unlock()
		if !authed {
			m.udpAllowDrops.Add(1)
			continue
		}
		if !m.allowInboundRate(p) {
			m.inboundRateDrops.Add(1)
			continue
		}

		frame, err := DecodeMeshFrameExact(buf[:n])
		if err != nil || frame.Type != MeshTypeAudioRelay {
			continue
		}
		relay, err := DecodeAudioRelay(frame.Payload)
		if err != nil || len(relay.TxRadios) > maxMeshVoiceTxRadios {
			continue
		}
		if relay.OriginNode != peerID {
			continue
		}
		m.voiceRx.Add(1)
		p.voiceRxCount.Add(1)
		p.lastVoiceUnixNano.Store(time.Now().UnixNano())

		m.mu.RLock()
		fn := m.onAudio
		m.mu.RUnlock()
		if fn != nil {
			fn(peerID, relay)
		}
	}
}

func (m *HybridMesh) allowInboundRate(p *hybridPeer) bool {
	p.rateMu.Lock()
	defer p.rateMu.Unlock()
	now := time.Now()
	if p.rateWindow.IsZero() || now.Sub(p.rateWindow) >= time.Second {
		p.rateWindow = now
		p.rateCount = 1
		return true
	}
	if p.rateCount >= meshVoiceInboundRatePerSec {
		return false
	}
	p.rateCount++
	return true
}

func (m *HybridMesh) voiceDrainLoop(ctx context.Context, peerID string) {
	// TryRecv + short sleep so queue replace on reconnect never blocks forever
	// on an abandoned channel (H-9 dual-conn replace).
	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}
		m.mu.RLock()
		p := m.peers[peerID]
		sendAddr := m.sendVoice[peerID]
		pc := m.voicePC
		m.mu.RUnlock()
		if p == nil || pc == nil {
			return
		}
		p.mu.Lock()
		q := p.voiceQ
		authed := p.authed
		p.mu.Unlock()
		if !authed || q == nil {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
			continue
		}
		job, ok := q.TryRecv()
		if !ok {
			select {
			case <-m.stopCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Millisecond):
			}
			continue
		}
		if sendAddr == nil {
			continue
		}
		_, err := pc.WriteTo(job.packet, sendAddr)
		if err != nil {
			m.udpSendErrs.Add(1)
			m.logger.Debug("AFV hybrid mesh UDP WriteTo", "peer", peerID, "err", err)
			// UDP errors never alone mark death (H-8)
			continue
		}
		m.voiceTx.Add(1)
		p.voiceTxCount.Add(1)
		p.lastVoiceUnixNano.Store(time.Now().UnixNano())
	}
}

func (m *HybridMesh) muteWatchLoop(ctx context.Context) {
	// Design Issue 15: ≤1/min Debug if Interest non-empty and no voice for >30s.
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	// per-peer last mute log time (unix nano)
	lastLog := make(map[string]int64)
	const muteSilence = 30 * time.Second
	const muteLogMin = time.Minute

	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Snapshot under m.mu only — never nest p.mu.
			type snap struct {
				id  string
				p   *hybridPeer
				has bool
			}
			m.mu.RLock()
			snaps := make([]snap, 0, len(m.peers))
			for id, p := range m.peers {
				snaps = append(snaps, snap{id: id, p: p, has: len(m.peerInterest[id]) > 0})
			}
			m.mu.RUnlock()

			now := time.Now()
			nowN := now.UnixNano()
			for _, s := range snaps {
				if !s.has {
					continue
				}
				s.p.mu.Lock()
				authed := s.p.authed
				s.p.mu.Unlock()
				if !authed {
					continue
				}
				lastV := s.p.lastVoiceUnixNano.Load()
				// No activity yet since connect, or silence > 30s
				silent := lastV == 0 || nowN-lastV > int64(muteSilence)
				if !silent {
					continue
				}
				if prev, ok := lastLog[s.id]; ok && nowN-prev < int64(muteLogMin) {
					continue
				}
				lastLog[s.id] = nowN
				m.logger.Debug("AFV hybrid mesh silent mute watch: Interest non-empty but no voice tx/rx for >30s",
					"peer", s.id)
			}
		}
	}
}
