package afv

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/renorris/openfsd/pkg/afvprotocol"
)

// Transceiver is a posted radio (PascalCase JSON wire fields).
type Transceiver struct {
	ID         uint16  `json:"ID"`
	Frequency  uint32  `json:"Frequency"` // Hz
	LatDeg     float64 `json:"LatDeg"`
	LonDeg     float64 `json:"LonDeg"`
	HeightMslM float64 `json:"HeightMslM"`
	HeightAglM float64 `json:"HeightAglM"`
}

// VoiceSession is an ephemeral voice channel (P0: memory only).
type VoiceSession struct {
	ChannelTag   string
	CID          int
	Callsign     string // display form
	CallsignKey  string // upper for uniqueness
	ClientName   string
	ClientTxKey  [afvprotocol.KeySize]byte // JSON aeadTransmitKey — server decrypt
	ClientRxKey  [afvprotocol.KeySize]byte // JSON aeadReceiveKey — server encrypt
	UDPAddr      net.Addr
	Bound        bool
	Transceivers []Transceiver // immutable replace
	RxSeq        *afvprotocol.SequenceWindow
	rxMu         sync.Mutex // guards RxSeq.Received
	TxSeq        atomic.Uint64
	LastUDP      atomic.Int64 // unix nano
	Created      time.Time
	IsATC        bool
	CrossCouple  bool
}

// acceptSeq records an inbound CryptoDTO sequence under the session lock.
func (s *VoiceSession) acceptSeq(seq uint64) afvprotocol.ReceiveOutcome {
	if s == nil || s.RxSeq == nil {
		return afvprotocol.ReceiveBefore
	}
	s.rxMu.Lock()
	defer s.rxMu.Unlock()
	return s.RxSeq.Received(seq)
}

func (s *VoiceSession) touchUDP(now time.Time) {
	if s != nil {
		s.LastUDP.Store(now.UnixNano())
	}
}

func (s *VoiceSession) lastUDPTime() time.Time {
	if s == nil {
		return time.Time{}
	}
	ns := s.LastUDP.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// nextTxSeq returns the next server→client CryptoDTO sequence.
func (s *VoiceSession) nextTxSeq() uint64 {
	return s.TxSeq.Add(1) - 1
}
