package afv

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

// runUDP listens for CryptoDTO voice packets until ctx is done.
func (s *Server) runUDP(ctx context.Context) error {
	addr := s.cfg.UDPListen
	if addr == "" {
		addr = "0.0.0.0:50000"
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	s.udpMu.Lock()
	s.udpConn = pc
	s.udpMu.Unlock()
	defer func() {
		_ = pc.Close()
		s.udpMu.Lock()
		s.udpConn = nil
		s.udpMu.Unlock()
	}()

	slog.Info("AFV UDP voice listening", "addr", pc.LocalAddr().String())

	maxDG := s.cfg.MaxDatagram
	if maxDG <= 0 {
		maxDG = 8192
	}
	buf := make([]byte, maxDG)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = pc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				slog.Debug("AFV UDP read", "err", err)
				continue
			}
		}
		if n > maxDG {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		s.handleUDP(pc, pkt, src)
	}
}

func (s *Server) handleUDP(pc net.PacketConn, pkt []byte, src net.Addr) {
	hdr, headerEnd, err := afvprotocol.ParseHeader(pkt)
	if err != nil {
		return
	}
	if hdr.Mode != afvprotocol.ModeChaCha20Poly1305 {
		return
	}
	sess := s.reg.LookupByTag(hdr.ChannelTag)
	if sess == nil {
		return
	}

	ch, err := afvprotocol.ServerChannel(sess.ChannelTag, sess.ClientRxKey[:], sess.ClientTxKey[:])
	if err != nil {
		return
	}
	body, err := ch.DecryptBody(pkt, headerEnd, hdr.Sequence)
	if err != nil {
		return
	}

	if sess.acceptSeq(hdr.Sequence) == afvprotocol.ReceiveBefore {
		return
	}

	dtoName, payload, err := afvprotocol.ParseBody(body)
	if err != nil {
		return
	}

	now := time.Now()
	if _, ok := s.reg.BindUDP(sess, src, now); !ok {
		return
	}
	sess.touchUDP(now)

	switch dtoName {
	case afvprotocol.DTONameHeartbeat:
		s.handleHeartbeat(pc, sess, payload)
	case afvprotocol.DTONameAudioTx:
		s.handleAudioTx(pc, sess, payload)
	}
}

func (s *Server) handleHeartbeat(pc net.PacketConn, sess *VoiceSession, payload []byte) {
	if len(payload) > 0 {
		if _, err := afvprotocol.DecodeHeartbeat(payload); err != nil {
			return
		}
	}
	ch, err := afvprotocol.ServerChannel(sess.ChannelTag, sess.ClientRxKey[:], sess.ClientTxKey[:])
	if err != nil {
		return
	}
	seq := sess.nextTxSeq()
	pkt, err := ch.EncapsulateHA(seq, nil)
	if err != nil {
		return
	}
	if sess.UDPAddr == nil {
		return
	}
	_, _ = pc.WriteTo(pkt, sess.UDPAddr)
}

func (s *Server) handleAudioTx(pc net.PacketConn, sess *VoiceSession, payload []byte) {
	at, err := afvprotocol.DecodeAudioTx(payload)
	if err != nil {
		return
	}
	if !callsignMatch(at.Callsign, sess.Callsign) {
		return
	}
	recipients := s.reg.routeAT(sess, at)
	for _, rec := range recipients {
		if rec.udp == nil || rec.sess == nil {
			continue
		}
		ar := afvprotocol.AudioRx{
			Callsign:        sess.Callsign,
			SequenceCounter: at.SequenceCounter,
			Audio:           at.Audio,
			LastPacket:      at.LastPacket,
			Transceivers:    rec.rx,
		}
		// Encrypt AR with recipient aeadReceiveKey (ClientRxKey).
		ch, err := afvprotocol.ServerChannel(rec.tag, rec.rxKey[:], rec.rxKey[:])
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

// LocalUDPAddr returns the bound UDP address string (for tests).
func (s *Server) LocalUDPAddr() string {
	s.udpMu.Lock()
	defer s.udpMu.Unlock()
	if s.udpConn == nil {
		return ""
	}
	return s.udpConn.LocalAddr().String()
}
