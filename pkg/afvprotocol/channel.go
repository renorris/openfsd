package afvprotocol

import (
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/chacha20poly1305"
)

var (
	errMode            = errors.New("afvprotocol: unsupported crypto mode")
	errDecrypt         = errors.New("afvprotocol: decrypt failed")
	errBodyShort       = errors.New("afvprotocol: body too short")
	errDTOLen          = errors.New("afvprotocol: dto length mismatch")
	errKeySize         = errors.New("afvprotocol: key must be 32 bytes")
	errCiphertextShort = errors.New("afvprotocol: ciphertext too short for tag")
)

// Channel holds AEAD keys from the *server* view after PostCallsignResponse:
//
//	DecryptKey = client's aeadTransmitKey (client→server)
//	EncryptKey = client's aeadReceiveKey  (server→client)
type Channel struct {
	ChannelTag string
	EncryptKey [KeySize]byte // aeadReceiveKey from JSON
	DecryptKey [KeySize]byte // aeadTransmitKey from JSON
}

// NewChannel builds a Channel from server-view key material.
func NewChannel(tag string, encryptKey, decryptKey []byte) (*Channel, error) {
	if len(encryptKey) != KeySize || len(decryptKey) != KeySize {
		return nil, errKeySize
	}
	c := &Channel{ChannelTag: tag}
	copy(c.EncryptKey[:], encryptKey)
	copy(c.DecryptKey[:], decryptKey)
	return c, nil
}

// NonceFromSequence builds the 12-byte ChaCha20-Poly1305 nonce:
// zeros[0:4] || sequence as u64 LE in bytes[4:12].
func NonceFromSequence(seq uint64) [NonceSize]byte {
	var n [NonceSize]byte
	binary.LittleEndian.PutUint64(n[4:], seq)
	return n
}

// DecryptBody decrypts the AEAD body after headerEnd.
// AAD = pkt[0:headerEnd]. Mode must be ChaCha20-Poly1305.
func (c *Channel) DecryptBody(pkt []byte, headerEnd int, seq uint64) ([]byte, error) {
	if c == nil {
		return nil, errKeySize
	}
	if headerEnd < 2 || headerEnd > len(pkt) {
		return nil, errPacketTooShort
	}
	ct := pkt[headerEnd:]
	if len(ct) < TagSize {
		return nil, errCiphertextShort
	}
	// Key is always KeySize (32); New only fails on wrong length.
	aead, err := chacha20poly1305.New(c.DecryptKey[:])
	if err != nil {
		return nil, errDecrypt
	}
	nonce := NonceFromSequence(seq)
	aad := pkt[:headerEnd]
	plain, err := aead.Open(nil, nonce[:], ct, aad)
	if err != nil {
		return nil, errDecrypt
	}
	return plain, nil
}

// ParseBody splits a decrypted body into dtoName and msgpack payload.
// Body: nameLen u16 LE | name | dtoMsgpackLen u16 LE | msgpack.
// dtoMsgpackLen must equal remaining after the length field (AFV-Native check).
func ParseBody(body []byte) (dtoName string, msgpackPayload []byte, err error) {
	if len(body) < 2 {
		return "", nil, errBodyShort
	}
	nameLen := int(binary.LittleEndian.Uint16(body[0:2]))
	if nameLen < 0 || 2+nameLen > len(body) {
		return "", nil, errBodyShort
	}
	dtoName = string(body[2 : 2+nameLen])
	rest := body[2+nameLen:]
	if len(rest) < 2 {
		return "", nil, errBodyShort
	}
	dtoLen := int(binary.LittleEndian.Uint16(rest[0:2]))
	if dtoLen != len(rest)-2 {
		return "", nil, errDTOLen
	}
	if dtoLen == 0 {
		return dtoName, nil, nil
	}
	return dtoName, rest[2:], nil
}

// EncodeBody builds nameLen|name|dtoMsgpackLen|msgpack.
// When msgpackPayload is nil/empty, dtoMsgpackLen=0 (normative HA).
func EncodeBody(dtoName string, msgpackPayload []byte) []byte {
	name := []byte(dtoName)
	mpLen := len(msgpackPayload)
	out := make([]byte, 2+len(name)+2+mpLen)
	binary.LittleEndian.PutUint16(out[0:2], uint16(len(name)))
	copy(out[2:], name)
	off := 2 + len(name)
	binary.LittleEndian.PutUint16(out[off:off+2], uint16(mpLen))
	if mpLen > 0 {
		copy(out[off+2:], msgpackPayload)
	}
	return out
}

// Encapsulate builds a full CryptoDTO datagram (header + AEAD body).
// Encrypts with EncryptKey (server→client = aeadReceiveKey).
// Mode is always ChaCha20-Poly1305.
// When msgpackPayload is nil/empty, dtoMsgpackLen=0.
// If dst has enough capacity it is reused; otherwise a new buffer is allocated.
// The returned slice is always the complete packet.
func (c *Channel) Encapsulate(seq uint64, dtoName string, msgpackPayload []byte, dst []byte) ([]byte, error) {
	if c == nil {
		return nil, errKeySize
	}
	body := EncodeBody(dtoName, msgpackPayload)
	hdr := Header{
		ChannelTag: c.ChannelTag,
		Sequence:   seq,
		Mode:       ModeChaCha20Poly1305,
	}
	prefix := PackHeaderPrefix(hdr, nil)
	aead, err := chacha20poly1305.New(c.EncryptKey[:])
	if err != nil {
		// Unreachable with [KeySize]byte keys; kept for API safety.
		return nil, errKeySize
	}
	nonce := NonceFromSequence(seq)
	// ciphertext || tag
	sealed := aead.Seal(nil, nonce[:], body, prefix)
	need := len(prefix) + len(sealed)
	if cap(dst) < need {
		dst = make([]byte, need)
	} else {
		dst = dst[:need]
	}
	copy(dst, prefix)
	copy(dst[len(prefix):], sealed)
	return dst, nil
}

// EncapsulateHA is a convenience for normative heartbeat ack (dtoMsgpackLen=0).
func (c *Channel) EncapsulateHA(seq uint64, dst []byte) ([]byte, error) {
	return c.Encapsulate(seq, DTONameHeartbeatAck, nil, dst)
}

// Decapsulate is a client/server helper: parse header, decrypt with DecryptKey,
// parse body. Rejects Mode != ChaCha20Poly1305.
func (c *Channel) Decapsulate(pkt []byte) (hdr Header, dtoName string, msgpackPayload []byte, err error) {
	hdr, headerEnd, err := ParseHeader(pkt)
	if err != nil {
		return Header{}, "", nil, err
	}
	if hdr.Mode != ModeChaCha20Poly1305 {
		return Header{}, "", nil, errMode
	}
	body, err := c.DecryptBody(pkt, headerEnd, hdr.Sequence)
	if err != nil {
		return Header{}, "", nil, err
	}
	dtoName, msgpackPayload, err = ParseBody(body)
	if err != nil {
		return Header{}, "", nil, err
	}
	return hdr, dtoName, msgpackPayload, nil
}

// ClientChannel builds a Channel from the *client* perspective ChannelConfig keys
// (transmit encrypts outbound, receive decrypts inbound). Useful for e2e tests.
func ClientChannel(tag string, aeadReceiveKey, aeadTransmitKey []byte) (*Channel, error) {
	// Client encrypts with Transmit, decrypts with Receive.
	// Channel.EncryptKey is used by Encapsulate; DecryptKey by Decapsulate.
	return NewChannel(tag, aeadTransmitKey, aeadReceiveKey)
}

// ServerChannel builds a Channel from PostCallsignResponse keys (server view):
// encrypt with aeadReceiveKey, decrypt with aeadTransmitKey.
func ServerChannel(tag string, aeadReceiveKey, aeadTransmitKey []byte) (*Channel, error) {
	return NewChannel(tag, aeadReceiveKey, aeadTransmitKey)
}
