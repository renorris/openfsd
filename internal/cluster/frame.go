package cluster

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frame type bytes (KD-2).
const (
	TypeHello             byte = 1
	TypeHeartbeat         byte = 2
	TypeClaimReserve      byte = 10
	TypeClaimReserveAck   byte = 11
	TypeClaimReserveNack  byte = 12
	TypeClaimCommit       byte = 13
	TypeClaimCommitAck    byte = 14
	TypeClaimCommitNack   byte = 15
	TypeClaimAbort        byte = 16
	TypeClaimRelease      byte = 17
	TypeDirectorySnapshot byte = 20
	TypeDirectoryDelta    byte = 21
	TypeJoinLeaveWire     byte = 30
	TypePositionBatch     byte = 31
	TypeDirectPacket      byte = 32
	TypeHomeRPCReq        byte = 40
	TypeHomeRPCResp       byte = 41
	TypeInterestUpdate    byte = 50
	TypeProximityHint     byte = 51
	TypeBroadcast         byte = 52
	TypeTextRanged        byte = 53 // reliable ranged text (not position coalesce)
)

const (
	// MaxFramePayload is the maximum payload size (4 MiB).
	MaxFramePayload = 4 << 20
	// frameHeaderLen = u32 length (payload+1 type) is not used; we use
	// [u32 be total payload len including type byte][u8 type][payload]
	// where u32 is len(type+payload).
	maxFrameTotal = MaxFramePayload + 1
)

var (
	ErrFrameTooLarge = errors.New("cluster: frame too large")
	ErrShortFrame    = errors.New("cluster: short frame")
)

// Frame is a length-prefixed mesh message.
type Frame struct {
	Type    byte
	Payload []byte
}

// EncodeFrame writes [u32 be len][u8 type][payload] where len = 1+len(payload).
func EncodeFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxFramePayload {
		return ErrFrameTooLarge
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

// DecodeFrame reads one frame from r.
func DecodeFrame(r io.Reader) (Frame, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[0:4])
	if n == 0 {
		return Frame{}, ErrShortFrame
	}
	if n > maxFrameTotal {
		return Frame{}, ErrFrameTooLarge
	}
	typ := hdr[4]
	payLen := int(n) - 1
	if payLen < 0 {
		return Frame{}, ErrShortFrame
	}
	var payload []byte
	if payLen > 0 {
		payload = make([]byte, payLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return Frame{}, err
		}
	}
	return Frame{Type: typ, Payload: payload}, nil
}

// encodeString encodes a length-prefixed string (u16 be + bytes).
func encodeString(b []byte, s string) []byte {
	if len(s) > 0xffff {
		s = s[:0xffff]
	}
	var hdr [2]byte
	binary.BigEndian.PutUint16(hdr[:], uint16(len(s)))
	b = append(b, hdr[:]...)
	b = append(b, s...)
	return b
}

func decodeString(b []byte) (string, []byte, error) {
	if len(b) < 2 {
		return "", nil, fmt.Errorf("cluster: short string")
	}
	n := int(binary.BigEndian.Uint16(b[:2]))
	b = b[2:]
	if len(b) < n {
		return "", nil, fmt.Errorf("cluster: short string body")
	}
	return string(b[:n]), b[n:], nil
}

func encodeBytes(b, p []byte) []byte {
	if len(p) > 0xffffff {
		p = p[:0xffffff]
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(p)))
	b = append(b, hdr[:]...)
	b = append(b, p...)
	return b
}

func decodeBytes(b []byte) ([]byte, []byte, error) {
	if len(b) < 4 {
		return nil, nil, fmt.Errorf("cluster: short bytes")
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	b = b[4:]
	if len(b) < n {
		return nil, nil, fmt.Errorf("cluster: short bytes body")
	}
	out := make([]byte, n)
	copy(out, b[:n])
	return out, b[n:], nil
}

func encodeU64(b []byte, v uint64) []byte {
	var hdr [8]byte
	binary.BigEndian.PutUint64(hdr[:], v)
	return append(b, hdr[:]...)
}

func decodeU64(b []byte) (uint64, []byte, error) {
	if len(b) < 8 {
		return 0, nil, fmt.Errorf("cluster: short u64")
	}
	return binary.BigEndian.Uint64(b[:8]), b[8:], nil
}

func encodeU32(b []byte, v uint32) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], v)
	return append(b, hdr[:]...)
}

func decodeU32(b []byte) (uint32, []byte, error) {
	if len(b) < 4 {
		return 0, nil, fmt.Errorf("cluster: short u32")
	}
	return binary.BigEndian.Uint32(b[:4]), b[4:], nil
}

func encodeF64(b []byte, v float64) []byte {
	return encodeU64(b, float64bits(v))
}

func decodeF64(b []byte) (float64, []byte, error) {
	u, rest, err := decodeU64(b)
	if err != nil {
		return 0, nil, err
	}
	return float64frombits(u), rest, nil
}

func float64bits(f float64) uint64 {
	return mathFloat64bits(f)
}

func float64frombits(b uint64) float64 {
	return mathFloat64frombits(b)
}
