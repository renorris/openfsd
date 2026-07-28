package afvprotocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Minimal hand-rolled MessagePack codec for AFV array DTOs only.
// Multi-byte integers inside msgpack are big-endian (MessagePack spec).
// Framing lengths outside msgpack use little-endian (AFV CryptoDTO).

var (
	errMsgpackTrunc       = errors.New("afvprotocol: msgpack truncated")
	errMsgpackType        = errors.New("afvprotocol: msgpack unexpected type")
	errMsgpackOverflow    = errors.New("afvprotocol: msgpack value overflow")
	errMsgpackUnsupported = errors.New("afvprotocol: msgpack unsupported type")
)

// encodeFixArrayHeader appends a fixarray or array16/array32 header for n elements.
func encodeArrayHeader(dst []byte, n int) []byte {
	if n < 0 {
		n = 0
	}
	if n <= 15 {
		return append(dst, 0x90|byte(n))
	}
	if n <= 0xffff {
		dst = append(dst, 0xdc)
		return binary.BigEndian.AppendUint16(dst, uint16(n))
	}
	dst = append(dst, 0xdd)
	return binary.BigEndian.AppendUint32(dst, uint32(n))
}

func encodeString(dst []byte, s string) []byte {
	n := len(s)
	if n <= 31 {
		dst = append(dst, 0xa0|byte(n))
		return append(dst, s...)
	}
	if n <= 0xff {
		dst = append(dst, 0xd9, byte(n))
		return append(dst, s...)
	}
	if n <= 0xffff {
		dst = append(dst, 0xda)
		dst = binary.BigEndian.AppendUint16(dst, uint16(n))
		return append(dst, s...)
	}
	dst = append(dst, 0xdb)
	dst = binary.BigEndian.AppendUint32(dst, uint32(n))
	return append(dst, s...)
}

func encodeUint64(dst []byte, v uint64) []byte {
	// Prefer the smallest representation that msgpack-c would typically use.
	switch {
	case v <= 0x7f:
		return append(dst, byte(v)) // positive fixint
	case v <= 0xff:
		return append(dst, 0xcc, byte(v))
	case v <= 0xffff:
		dst = append(dst, 0xcd)
		return binary.BigEndian.AppendUint16(dst, uint16(v))
	case v <= 0xffffffff:
		dst = append(dst, 0xce)
		return binary.BigEndian.AppendUint32(dst, uint32(v))
	default:
		dst = append(dst, 0xcf)
		return binary.BigEndian.AppendUint64(dst, v)
	}
}

func encodeInt(dst []byte, v int64) []byte {
	if v >= 0 {
		return encodeUint64(dst, uint64(v))
	}
	// negative
	if v >= -32 {
		return append(dst, byte(int8(v))) // negative fixint
	}
	if v >= math.MinInt8 {
		return append(dst, 0xd0, byte(int8(v)))
	}
	if v >= math.MinInt16 {
		dst = append(dst, 0xd1)
		return binary.BigEndian.AppendUint16(dst, uint16(int16(v)))
	}
	if v >= math.MinInt32 {
		dst = append(dst, 0xd2)
		return binary.BigEndian.AppendUint32(dst, uint32(int32(v)))
	}
	dst = append(dst, 0xd3)
	return binary.BigEndian.AppendUint64(dst, uint64(v))
}

func encodeBool(dst []byte, v bool) []byte {
	if v {
		return append(dst, 0xc3)
	}
	return append(dst, 0xc2)
}

func encodeBin(dst []byte, b []byte) []byte {
	n := len(b)
	switch {
	case n <= 0xff:
		dst = append(dst, 0xc4, byte(n))
	case n <= 0xffff:
		dst = append(dst, 0xc5)
		dst = binary.BigEndian.AppendUint16(dst, uint16(n))
	default:
		dst = append(dst, 0xc6)
		dst = binary.BigEndian.AppendUint32(dst, uint32(n))
	}
	return append(dst, b...)
}

func encodeFloat32(dst []byte, v float32) []byte {
	dst = append(dst, 0xca)
	return binary.BigEndian.AppendUint32(dst, math.Float32bits(v))
}

// decoder is a cursor over msgpack bytes.
type decoder struct {
	b []byte
	i int
}

func (d *decoder) remain() int { return len(d.b) - d.i }

func (d *decoder) u8() (byte, error) {
	if d.i >= len(d.b) {
		return 0, errMsgpackTrunc
	}
	v := d.b[d.i]
	d.i++
	return v, nil
}

func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || d.i+n > len(d.b) {
		return nil, errMsgpackTrunc
	}
	out := d.b[d.i : d.i+n]
	d.i += n
	return out, nil
}

func (d *decoder) peek() (byte, error) {
	if d.i >= len(d.b) {
		return 0, errMsgpackTrunc
	}
	return d.b[d.i], nil
}

func (d *decoder) arrayLen() (int, error) {
	b, err := d.u8()
	if err != nil {
		return 0, err
	}
	switch {
	case b&0xf0 == 0x90:
		return int(b & 0x0f), nil
	case b == 0xdc:
		raw, err := d.take(2)
		if err != nil {
			return 0, err
		}
		return int(binary.BigEndian.Uint16(raw)), nil
	case b == 0xdd:
		raw, err := d.take(4)
		if err != nil {
			return 0, err
		}
		return int(binary.BigEndian.Uint32(raw)), nil
	default:
		return 0, fmt.Errorf("%w: array header 0x%02x", errMsgpackType, b)
	}
}

func (d *decoder) string() (string, error) {
	b, err := d.u8()
	if err != nil {
		return "", err
	}
	var n int
	switch {
	case b&0xe0 == 0xa0:
		n = int(b & 0x1f)
	case b == 0xd9:
		nb, err := d.u8()
		if err != nil {
			return "", err
		}
		n = int(nb)
	case b == 0xda:
		raw, err := d.take(2)
		if err != nil {
			return "", err
		}
		n = int(binary.BigEndian.Uint16(raw))
	case b == 0xdb:
		raw, err := d.take(4)
		if err != nil {
			return "", err
		}
		n = int(binary.BigEndian.Uint32(raw))
	default:
		return "", fmt.Errorf("%w: string header 0x%02x", errMsgpackType, b)
	}
	raw, err := d.take(n)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (d *decoder) uint64() (uint64, error) {
	b, err := d.u8()
	if err != nil {
		return 0, err
	}
	switch {
	case b <= 0x7f:
		return uint64(b), nil
	case b >= 0xe0: // negative fixint — treat as signed then cast
		return 0, fmt.Errorf("%w: negative int where uint expected", errMsgpackType)
	case b == 0xcc:
		v, err := d.u8()
		return uint64(v), err
	case b == 0xcd:
		raw, err := d.take(2)
		if err != nil {
			return 0, err
		}
		return uint64(binary.BigEndian.Uint16(raw)), nil
	case b == 0xce:
		raw, err := d.take(4)
		if err != nil {
			return 0, err
		}
		return uint64(binary.BigEndian.Uint32(raw)), nil
	case b == 0xcf:
		raw, err := d.take(8)
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint64(raw), nil
	case b == 0xd0: // int8
		v, err := d.u8()
		if err != nil {
			return 0, err
		}
		iv := int8(v)
		if iv < 0 {
			return 0, errMsgpackOverflow
		}
		return uint64(iv), nil
	case b == 0xd1:
		raw, err := d.take(2)
		if err != nil {
			return 0, err
		}
		iv := int16(binary.BigEndian.Uint16(raw))
		if iv < 0 {
			return 0, errMsgpackOverflow
		}
		return uint64(iv), nil
	case b == 0xd2:
		raw, err := d.take(4)
		if err != nil {
			return 0, err
		}
		iv := int32(binary.BigEndian.Uint32(raw))
		if iv < 0 {
			return 0, errMsgpackOverflow
		}
		return uint64(iv), nil
	case b == 0xd3:
		raw, err := d.take(8)
		if err != nil {
			return 0, err
		}
		iv := int64(binary.BigEndian.Uint64(raw))
		if iv < 0 {
			return 0, errMsgpackOverflow
		}
		return uint64(iv), nil
	default:
		return 0, fmt.Errorf("%w: uint header 0x%02x", errMsgpackType, b)
	}
}

func (d *decoder) int() (int64, error) {
	b, err := d.peek()
	if err != nil {
		return 0, err
	}
	// positive path
	if b <= 0x7f || b == 0xcc || b == 0xcd || b == 0xce || b == 0xcf {
		u, err := d.uint64()
		if err != nil {
			return 0, err
		}
		if u > math.MaxInt64 {
			return 0, errMsgpackOverflow
		}
		return int64(u), nil
	}
	// negative fixint
	if b >= 0xe0 {
		_, _ = d.u8()
		return int64(int8(b)), nil
	}
	_, _ = d.u8()
	switch b {
	case 0xd0:
		v, err := d.u8()
		return int64(int8(v)), err
	case 0xd1:
		raw, err := d.take(2)
		if err != nil {
			return 0, err
		}
		return int64(int16(binary.BigEndian.Uint16(raw))), nil
	case 0xd2:
		raw, err := d.take(4)
		if err != nil {
			return 0, err
		}
		return int64(int32(binary.BigEndian.Uint32(raw))), nil
	case 0xd3:
		raw, err := d.take(8)
		if err != nil {
			return 0, err
		}
		return int64(binary.BigEndian.Uint64(raw)), nil
	default:
		return 0, fmt.Errorf("%w: int header 0x%02x", errMsgpackType, b)
	}
}

func (d *decoder) bool() (bool, error) {
	b, err := d.u8()
	if err != nil {
		return false, err
	}
	switch b {
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	default:
		return false, fmt.Errorf("%w: bool header 0x%02x", errMsgpackType, b)
	}
}

func (d *decoder) bin() ([]byte, error) {
	b, err := d.u8()
	if err != nil {
		return nil, err
	}
	var n int
	switch b {
	case 0xc4:
		nb, err := d.u8()
		if err != nil {
			return nil, err
		}
		n = int(nb)
	case 0xc5:
		raw, err := d.take(2)
		if err != nil {
			return nil, err
		}
		n = int(binary.BigEndian.Uint16(raw))
	case 0xc6:
		raw, err := d.take(4)
		if err != nil {
			return nil, err
		}
		n = int(binary.BigEndian.Uint32(raw))
	default:
		// AFV may also pack audio as a str (older clients) — accept str as bin.
		d.i-- // rewind
		s, err := d.string()
		if err != nil {
			return nil, fmt.Errorf("%w: bin header 0x%02x", errMsgpackType, b)
		}
		return []byte(s), nil
	}
	return d.take(n)
}

func (d *decoder) float32() (float32, error) {
	b, err := d.u8()
	if err != nil {
		return 0, err
	}
	switch b {
	case 0xca:
		raw, err := d.take(4)
		if err != nil {
			return 0, err
		}
		return math.Float32frombits(binary.BigEndian.Uint32(raw)), nil
	case 0xcb:
		raw, err := d.take(8)
		if err != nil {
			return 0, err
		}
		return float32(math.Float64frombits(binary.BigEndian.Uint64(raw))), nil
	default:
		// Accept integer zero/one as float for robustness.
		d.i--
		iv, err := d.int()
		if err != nil {
			return 0, fmt.Errorf("%w: float header 0x%02x", errMsgpackType, b)
		}
		return float32(iv), nil
	}
}

func (d *decoder) uint16() (uint16, error) {
	u, err := d.uint64()
	if err != nil {
		return 0, err
	}
	if u > 0xffff {
		return 0, errMsgpackOverflow
	}
	return uint16(u), nil
}

func (d *decoder) uint32() (uint32, error) {
	u, err := d.uint64()
	if err != nil {
		return 0, err
	}
	if u > 0xffffffff {
		return 0, errMsgpackOverflow
	}
	return uint32(u), nil
}
