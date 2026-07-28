package afvprotocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Header is the plaintext CryptoDTO header (msgpack array).
// MSGPACK_DEFINE_ARRAY(ChannelTag, Sequence, Mode).
type Header struct {
	ChannelTag string
	Sequence   uint64
	Mode       int
}

var (
	errPacketTooShort = errors.New("afvprotocol: packet too short")
	errHeaderSize     = errors.New("afvprotocol: invalid header size")
	errHeaderMsgpack  = errors.New("afvprotocol: header msgpack decode failed")
)

// EncodeHeaderMsgpack encodes the header as a msgpack array of 3 elements.
func EncodeHeaderMsgpack(h Header) []byte {
	dst := make([]byte, 0, 32+len(h.ChannelTag))
	dst = encodeArrayHeader(dst, 3)
	dst = encodeString(dst, h.ChannelTag)
	dst = encodeUint64(dst, h.Sequence)
	dst = encodeInt(dst, int64(h.Mode))
	return dst
}

// DecodeHeaderMsgpack decodes a msgpack Header array.
func DecodeHeaderMsgpack(b []byte) (Header, error) {
	d := decoder{b: b}
	n, err := d.arrayLen()
	if err != nil {
		return Header{}, fmt.Errorf("%w: %v", errHeaderMsgpack, err)
	}
	if n != 3 {
		return Header{}, fmt.Errorf("%w: expected array len 3, got %d", errHeaderMsgpack, n)
	}
	tag, err := d.string()
	if err != nil {
		return Header{}, fmt.Errorf("%w: channelTag: %v", errHeaderMsgpack, err)
	}
	seq, err := d.uint64()
	if err != nil {
		return Header{}, fmt.Errorf("%w: sequence: %v", errHeaderMsgpack, err)
	}
	mode, err := d.int()
	if err != nil {
		return Header{}, fmt.Errorf("%w: mode: %v", errHeaderMsgpack, err)
	}
	return Header{ChannelTag: tag, Sequence: seq, Mode: int(mode)}, nil
}

// ParseHeader reads the plaintext CryptoDTO prefix: u16 LE headerSize + header msgpack.
// headerEnd is the absolute offset into pkt where the AEAD ciphertext begins (2+headerSize).
func ParseHeader(pkt []byte) (hdr Header, headerEnd int, err error) {
	if len(pkt) < 2 {
		return Header{}, 0, errPacketTooShort
	}
	headerSize := int(binary.LittleEndian.Uint16(pkt[0:2]))
	if headerSize <= 0 {
		return Header{}, 0, errHeaderSize
	}
	headerEnd = 2 + headerSize
	if len(pkt) < headerEnd {
		return Header{}, 0, errPacketTooShort
	}
	hdr, err = DecodeHeaderMsgpack(pkt[2:headerEnd])
	if err != nil {
		return Header{}, 0, err
	}
	return hdr, headerEnd, nil
}

// PackHeaderPrefix writes u16 LE headerSize || header msgpack into dst (or allocates).
func PackHeaderPrefix(h Header, dst []byte) []byte {
	mp := EncodeHeaderMsgpack(h)
	need := 2 + len(mp)
	if cap(dst) < need {
		dst = make([]byte, need)
	} else {
		dst = dst[:need]
	}
	binary.LittleEndian.PutUint16(dst[0:2], uint16(len(mp)))
	copy(dst[2:], mp)
	return dst
}
