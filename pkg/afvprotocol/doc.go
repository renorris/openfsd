// Package afvprotocol implements the AFV (Audio for VATSIM) CryptoDTO wire
// format: header msgpack, ChaCha20-Poly1305 body encapsulation, and voice DTOs
// (H/HA/AT/AR). It is pure: no I/O beyond crypto. Allowlist: stdlib +
// golang.org/x/crypto.
//
// Wire layout (all multi-byte framing integers are little-endian):
//
//	u16 LE headerSize | msgpack Header [ChannelTag, Sequence, Mode] | AEAD(ciphertext||tag)
//
// AAD = pkt[0:2+headerSize]. Nonce = zeros[0:4] || Sequence as u64 LE.
// Body after decrypt: nameLen u16 LE | name | dtoMsgpackLen u16 LE | msgpack.
//
// Protocol source of truth: AFV-Native (xsquawkbox/AFV-Native, BSD-3).
package afvprotocol
