package afv

// AFV multi-node mesh framing (PR-10 / KD-17).
//
// Hello auth (M-3): constant-time compare of shared PSK. Trust root and payload
// shape match FSD mesh Hello (nodeID + PSK strings); comparison is CT-upgraded
// vs FSD's non-constant-time !=. Do not log PSK contents on failure.
//
// Local length-prefix framing — do not import internal/cluster.

import (
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// Mesh frame type bytes (KD-17).
const (
	MeshTypeHello        byte = 1
	MeshTypeHeartbeat    byte = 2
	MeshTypeTrxSnapshot  byte = 10
	MeshTypeTrxDelta     byte = 11
	MeshTypeSessionLeave byte = 12
	MeshTypeAudioRelay   byte = 20
	MeshTypeInterest     byte = 30

	// MaxMeshPayload is the maximum payload size (1 MiB).
	MaxMeshPayload = 1 << 20
	// maxMeshString is max length for nodeID/callsign/tag fields.
	maxMeshString = 1024
	// maxAudioRelayBytes clamps opaque Opus on AudioRelay.
	maxAudioRelayBytes = 8192

	maxMeshFrameTotal = MaxMeshPayload + 1
)

var (
	errMeshFrameTooLarge = errors.New("afv mesh: frame too large")
	errMeshShortFrame    = errors.New("afv mesh: short frame")
	errMeshHelloAuth     = errors.New("afv mesh: hello auth failed")
	errMeshBadPayload    = errors.New("afv mesh: bad payload")
)

// MeshFrame is a length-prefixed mesh message.
type MeshFrame struct {
	Type    byte
	Payload []byte
}

// EncodeMeshFrame writes [u32 be len][u8 type][payload] where len = 1+len(payload).
func EncodeMeshFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxMeshPayload {
		return errMeshFrameTooLarge
	}
	var hdr [5]byte
	n := uint32(1 + len(payload))
	binary.BigEndian.PutUint32(hdr[0:4], n)
	hdr[4] = typ
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// DecodeMeshFrame reads one frame from r. Rejects n==0 and oversized payloads.
func DecodeMeshFrame(r io.Reader) (MeshFrame, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return MeshFrame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[0:4])
	if n == 0 {
		return MeshFrame{}, errMeshShortFrame
	}
	if n > maxMeshFrameTotal {
		return MeshFrame{}, errMeshFrameTooLarge
	}
	typ := hdr[4]
	// n >= 1 (n==0 rejected above) so payLen = n-1 is always >= 0.
	payLen := int(n) - 1
	var payload []byte
	if payLen > 0 {
		payload = make([]byte, payLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return MeshFrame{}, err
		}
	}
	return MeshFrame{Type: typ, Payload: payload}, nil
}

// --- binary helpers (local copies; no cluster import) ---

func meshEncodeString(b []byte, s string) []byte {
	if len(s) > maxMeshString {
		s = s[:maxMeshString]
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(s)))
	b = append(b, hdr[:]...)
	b = append(b, s...)
	return b
}

func meshDecodeString(b []byte) (string, []byte, error) {
	if len(b) < 2 {
		return "", nil, errMeshBadPayload
	}
	n := int(binary.BigEndian.Uint16(b[:2]))
	b = b[2:]
	if n > maxMeshString || len(b) < n {
		return "", nil, errMeshBadPayload
	}
	return string(b[:n]), b[n:], nil
}

func meshEncodeBytes(b, p []byte) []byte {
	if len(p) > 0xffffff {
		p = p[:0xffffff]
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(p)))
	b = append(b, hdr[:]...)
	b = append(b, p...)
	return b
}

func meshDecodeBytes(b []byte) ([]byte, []byte, error) {
	if len(b) < 4 {
		return nil, nil, errMeshBadPayload
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	b = b[4:]
	if n < 0 || len(b) < n {
		return nil, nil, errMeshBadPayload
	}
	out := make([]byte, n)
	copy(out, b[:n])
	return out, b[n:], nil
}

func meshEncodeU16(b []byte, v uint16) []byte {
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], v)
	return append(b, hdr[:]...)
}

func meshDecodeU16(b []byte) (uint16, []byte, error) {
	if len(b) < 2 {
		return 0, nil, errMeshBadPayload
	}
	return binary.BigEndian.Uint16(b[:2]), b[2:], nil
}

func meshEncodeU32(b []byte, v uint32) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], v)
	return append(b, hdr[:]...)
}

func meshDecodeU32(b []byte) (uint32, []byte, error) {
	if len(b) < 4 {
		return 0, nil, errMeshBadPayload
	}
	return binary.BigEndian.Uint32(b[:4]), b[4:], nil
}

func meshEncodeU64(b []byte, v uint64) []byte {
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], v)
	return append(b, hdr[:]...)
}

func meshDecodeU64(b []byte) (uint64, []byte, error) {
	if len(b) < 8 {
		return 0, nil, errMeshBadPayload
	}
	return binary.BigEndian.Uint64(b[:8]), b[8:], nil
}

func meshEncodeI32(b []byte, v int32) []byte {
	return meshEncodeU32(b, uint32(v))
}

func meshDecodeI32(b []byte) (int32, []byte, error) {
	u, rest, err := meshDecodeU32(b)
	if err != nil {
		return 0, nil, err
	}
	return int32(u), rest, nil
}

func meshEncodeF64(b []byte, v float64) []byte {
	return meshEncodeU64(b, math.Float64bits(v))
}

func meshDecodeF64(b []byte) (float64, []byte, error) {
	u, rest, err := meshDecodeU64(b)
	if err != nil {
		return 0, nil, err
	}
	return math.Float64frombits(u), rest, nil
}

func meshEncodeBoolU8(b []byte, v bool) []byte {
	if v {
		return append(b, 1)
	}
	return append(b, 0)
}

func meshDecodeBoolU8(b []byte) (bool, []byte, error) {
	if len(b) < 1 {
		return false, nil, errMeshBadPayload
	}
	return b[0] != 0, b[1:], nil
}

// --- Hello (type 1) ---

// HelloPayload is mutual Hello content (nodeID + PSK).
type HelloPayload struct {
	NodeID string
	PSK    string
}

// EncodeHelloPayload encodes Hello payload.
func EncodeHelloPayload(p HelloPayload) []byte {
	var b []byte
	b = meshEncodeString(b, p.NodeID)
	b = meshEncodeString(b, p.PSK)
	return b
}

// DecodeHelloPayload decodes Hello payload.
func DecodeHelloPayload(b []byte) (HelloPayload, error) {
	var p HelloPayload
	var err error
	p.NodeID, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	p.PSK, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	if len(b) != 0 {
		return p, errMeshBadPayload
	}
	return p, nil
}

// VerifyHelloPSK constant-time compares peer PSK to local. Length mismatch rejects
// without early return that leaks length via timing of compare body (dummy compare).
func VerifyHelloPSK(localPSK, peerPSK string) error {
	lb := []byte(localPSK)
	pb := []byte(peerPSK)
	if len(lb) != len(pb) {
		// Still run a dummy compare against local to avoid pure length short-circuit
		// being the only branch (best-effort; length still differs).
		_ = subtle.ConstantTimeCompare(lb, lb)
		return errMeshHelloAuth
	}
	if subtle.ConstantTimeCompare(lb, pb) != 1 {
		return errMeshHelloAuth
	}
	return nil
}

// --- Heartbeat (type 2) ---

// EncodeHeartbeatPayload encodes optional unix_ms; empty payload is also valid.
func EncodeHeartbeatPayload(unixMs uint64) []byte {
	return meshEncodeU64(nil, unixMs)
}

// DecodeHeartbeatPayload accepts empty (ok) or u64 unix_ms.
func DecodeHeartbeatPayload(b []byte) (unixMs uint64, err error) {
	if len(b) == 0 {
		return 0, nil
	}
	if len(b) < 8 {
		return 0, errMeshBadPayload
	}
	u, rest, err := meshDecodeU64(b)
	if err != nil {
		return 0, err
	}
	if len(rest) != 0 {
		return 0, errMeshBadPayload
	}
	return u, nil
}

// --- TrxSnapshot (type 10) ---

// MeshTrx is a radio on the mesh directory wire.
type MeshTrx struct {
	ID     uint16
	FreqHz uint32
	LatDeg float64
	LonDeg float64
	AltM   float64
}

// MeshSessionBlock is one session in a TrxSnapshot.
type MeshSessionBlock struct {
	Callsign   string
	ChannelTag string // informational only; not a crypto secret
	IsATC      bool
	Trxs       []MeshTrx
}

// TrxSnapshotPayload is type 10.
type TrxSnapshotPayload struct {
	OriginNodeID string
	Sessions     []MeshSessionBlock
}

// EncodeTrxSnapshot encodes a full remote directory snapshot for origin.
func EncodeTrxSnapshot(p TrxSnapshotPayload) []byte {
	var b []byte
	b = meshEncodeString(b, p.OriginNodeID)
	b = meshEncodeU32(b, uint32(len(p.Sessions)))
	for _, s := range p.Sessions {
		b = meshEncodeString(b, s.Callsign)
		b = meshEncodeString(b, s.ChannelTag)
		b = meshEncodeBoolU8(b, s.IsATC)
		b = meshEncodeU16(b, uint16(len(s.Trxs)))
		for _, t := range s.Trxs {
			b = meshEncodeU16(b, t.ID)
			b = meshEncodeU32(b, t.FreqHz)
			b = meshEncodeF64(b, t.LatDeg)
			b = meshEncodeF64(b, t.LonDeg)
			b = meshEncodeF64(b, t.AltM)
		}
	}
	return b
}

// DecodeTrxSnapshot decodes type 10.
func DecodeTrxSnapshot(b []byte) (TrxSnapshotPayload, error) {
	var p TrxSnapshotPayload
	var err error
	p.OriginNodeID, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	n, b, err := meshDecodeU32(b)
	if err != nil {
		return p, err
	}
	if n > 1<<20 {
		return p, errMeshBadPayload
	}
	p.Sessions = make([]MeshSessionBlock, 0, n)
	for i := uint32(0); i < n; i++ {
		var s MeshSessionBlock
		s.Callsign, b, err = meshDecodeString(b)
		if err != nil {
			return p, err
		}
		s.ChannelTag, b, err = meshDecodeString(b)
		if err != nil {
			return p, err
		}
		s.IsATC, b, err = meshDecodeBoolU8(b)
		if err != nil {
			return p, err
		}
		nt, bb, err := meshDecodeU16(b)
		if err != nil {
			return p, err
		}
		b = bb
		s.Trxs = make([]MeshTrx, 0, nt)
		for j := uint16(0); j < nt; j++ {
			var t MeshTrx
			t.ID, b, err = meshDecodeU16(b)
			if err != nil {
				return p, err
			}
			t.FreqHz, b, err = meshDecodeU32(b)
			if err != nil {
				return p, err
			}
			t.LatDeg, b, err = meshDecodeF64(b)
			if err != nil {
				return p, err
			}
			t.LonDeg, b, err = meshDecodeF64(b)
			if err != nil {
				return p, err
			}
			t.AltM, b, err = meshDecodeF64(b)
			if err != nil {
				return p, err
			}
			s.Trxs = append(s.Trxs, t)
		}
		p.Sessions = append(p.Sessions, s)
	}
	if len(b) != 0 {
		return p, errMeshBadPayload
	}
	return p, nil
}

// --- TrxDelta (type 11) ---

// TrxDeltaPayload is type 11 — replace-all radios for one callsign on origin.
type TrxDeltaPayload struct {
	OriginNodeID string
	Callsign     string
	IsATC        bool
	Trxs         []MeshTrx
}

// EncodeTrxDelta encodes type 11. Empty nTrx means session present with no radios.
func EncodeTrxDelta(p TrxDeltaPayload) []byte {
	var b []byte
	b = meshEncodeString(b, p.OriginNodeID)
	b = meshEncodeString(b, p.Callsign)
	b = meshEncodeBoolU8(b, p.IsATC)
	b = meshEncodeU16(b, uint16(len(p.Trxs)))
	for _, t := range p.Trxs {
		b = meshEncodeU16(b, t.ID)
		b = meshEncodeU32(b, t.FreqHz)
		b = meshEncodeF64(b, t.LatDeg)
		b = meshEncodeF64(b, t.LonDeg)
		b = meshEncodeF64(b, t.AltM)
	}
	return b
}

// DecodeTrxDelta decodes type 11.
func DecodeTrxDelta(b []byte) (TrxDeltaPayload, error) {
	var p TrxDeltaPayload
	var err error
	p.OriginNodeID, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	p.Callsign, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	p.IsATC, b, err = meshDecodeBoolU8(b)
	if err != nil {
		return p, err
	}
	nt, b, err := meshDecodeU16(b)
	if err != nil {
		return p, err
	}
	p.Trxs = make([]MeshTrx, 0, nt)
	for j := uint16(0); j < nt; j++ {
		var t MeshTrx
		t.ID, b, err = meshDecodeU16(b)
		if err != nil {
			return p, err
		}
		t.FreqHz, b, err = meshDecodeU32(b)
		if err != nil {
			return p, err
		}
		t.LatDeg, b, err = meshDecodeF64(b)
		if err != nil {
			return p, err
		}
		t.LonDeg, b, err = meshDecodeF64(b)
		if err != nil {
			return p, err
		}
		t.AltM, b, err = meshDecodeF64(b)
		if err != nil {
			return p, err
		}
		p.Trxs = append(p.Trxs, t)
	}
	if len(b) != 0 {
		return p, errMeshBadPayload
	}
	return p, nil
}

// --- SessionLeave (type 12) ---

// SessionLeavePayload is type 12.
type SessionLeavePayload struct {
	OriginNodeID string
	Callsign     string
}

// EncodeSessionLeave encodes type 12.
func EncodeSessionLeave(p SessionLeavePayload) []byte {
	var b []byte
	b = meshEncodeString(b, p.OriginNodeID)
	b = meshEncodeString(b, p.Callsign)
	return b
}

// DecodeSessionLeave decodes type 12.
func DecodeSessionLeave(b []byte) (SessionLeavePayload, error) {
	var p SessionLeavePayload
	var err error
	p.OriginNodeID, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	p.Callsign, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	if len(b) != 0 {
		return p, errMeshBadPayload
	}
	return p, nil
}

// --- AudioRelay (type 20) ---
// Forbidden fields: ClientTxKey, ClientRxKey, ChannelTag keys, JWT, passwords.
// AEAD keys never leave the home node.

// EncodeAudioRelay encodes type 20. Rejects audio longer than maxAudioRelayBytes.
func EncodeAudioRelay(r AudioRelay) ([]byte, error) {
	if len(r.Audio) > maxAudioRelayBytes {
		return nil, fmt.Errorf("afv mesh: audioLen %d > %d: %w", len(r.Audio), maxAudioRelayBytes, errMeshBadPayload)
	}
	var b []byte
	b = meshEncodeString(b, r.OriginNode)
	b = meshEncodeString(b, r.Callsign)
	b = meshEncodeU32(b, r.SequenceCounter)
	b = meshEncodeBoolU8(b, r.LastPacket)
	b = meshEncodeBoolU8(b, r.IsATC)
	b = meshEncodeBoolU8(b, r.IsXC)
	b = meshEncodeBytes(b, r.Audio)
	b = meshEncodeU16(b, uint16(len(r.TxRadios)))
	for _, t := range r.TxRadios {
		b = meshEncodeU16(b, t.TxID)
		b = meshEncodeU32(b, t.FreqHz)
		b = meshEncodeF64(b, t.LatDeg)
		b = meshEncodeF64(b, t.LonDeg)
		b = meshEncodeF64(b, t.HeightM)
	}
	return b, nil
}

// DecodeAudioRelay decodes type 20. Rejects audioLen > 8192.
func DecodeAudioRelay(b []byte) (AudioRelay, error) {
	var r AudioRelay
	var err error
	r.OriginNode, b, err = meshDecodeString(b)
	if err != nil {
		return r, err
	}
	r.Callsign, b, err = meshDecodeString(b)
	if err != nil {
		return r, err
	}
	r.SequenceCounter, b, err = meshDecodeU32(b)
	if err != nil {
		return r, err
	}
	r.LastPacket, b, err = meshDecodeBoolU8(b)
	if err != nil {
		return r, err
	}
	r.IsATC, b, err = meshDecodeBoolU8(b)
	if err != nil {
		return r, err
	}
	r.IsXC, b, err = meshDecodeBoolU8(b)
	if err != nil {
		return r, err
	}
	r.Audio, b, err = meshDecodeBytes(b)
	if err != nil {
		return r, err
	}
	if len(r.Audio) > maxAudioRelayBytes {
		return r, errMeshBadPayload
	}
	nt, b, err := meshDecodeU16(b)
	if err != nil {
		return r, err
	}
	r.TxRadios = make([]RelayTxRadio, 0, nt)
	for j := uint16(0); j < nt; j++ {
		var t RelayTxRadio
		t.TxID, b, err = meshDecodeU16(b)
		if err != nil {
			return r, err
		}
		t.FreqHz, b, err = meshDecodeU32(b)
		if err != nil {
			return r, err
		}
		t.LatDeg, b, err = meshDecodeF64(b)
		if err != nil {
			return r, err
		}
		t.LonDeg, b, err = meshDecodeF64(b)
		if err != nil {
			return r, err
		}
		t.HeightM, b, err = meshDecodeF64(b)
		if err != nil {
			return r, err
		}
		r.TxRadios = append(r.TxRadios, t)
	}
	if len(b) != 0 {
		return r, errMeshBadPayload
	}
	return r, nil
}

// --- Interest (type 30) ---

// InterestPayload is type 30 — full replace of interest set for nodeID.
type InterestPayload struct {
	NodeID  string
	Entries []InterestEntry
}

// EncodeInterest encodes type 30.
func EncodeInterest(p InterestPayload) []byte {
	var b []byte
	b = meshEncodeString(b, p.NodeID)
	b = meshEncodeU32(b, uint32(len(p.Entries)))
	for _, e := range p.Entries {
		b = meshEncodeU32(b, e.FreqHz)
		b = meshEncodeI32(b, e.ILat)
		b = meshEncodeI32(b, e.ILon)
	}
	return b
}

// DecodeInterest decodes type 30.
func DecodeInterest(b []byte) (InterestPayload, error) {
	var p InterestPayload
	var err error
	p.NodeID, b, err = meshDecodeString(b)
	if err != nil {
		return p, err
	}
	n, b, err := meshDecodeU32(b)
	if err != nil {
		return p, err
	}
	if n > 1<<20 {
		return p, errMeshBadPayload
	}
	p.Entries = make([]InterestEntry, 0, n)
	for i := uint32(0); i < n; i++ {
		var e InterestEntry
		e.FreqHz, b, err = meshDecodeU32(b)
		if err != nil {
			return p, err
		}
		e.ILat, b, err = meshDecodeI32(b)
		if err != nil {
			return p, err
		}
		e.ILon, b, err = meshDecodeI32(b)
		if err != nil {
			return p, err
		}
		p.Entries = append(p.Entries, e)
	}
	if len(b) != 0 {
		return p, errMeshBadPayload
	}
	return p, nil
}

// meshCallsignKey normalizes callsign for leave keys (upper).
func meshCallsignKey(cs string) string {
	return strings.ToUpper(strings.TrimSpace(cs))
}
