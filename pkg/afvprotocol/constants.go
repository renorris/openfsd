package afvprotocol

// NetworkVersion is the AFV-Native client network version UUID.
// Metadata only — not required in AuthRequest JSON.
const NetworkVersion = "3a5ddc6d-cf5d-4319-bd0e-d184f772db80"

// Crypto modes (AFV-Native CryptoDtoMode).
const (
	ModeUndefined        = 0
	ModeNone             = 1
	ModeChaCha20Poly1305 = 2
)

// AEAD sizes (bytes).
const (
	KeySize   = 32
	NonceSize = 12
	TagSize   = 16
)

// MaxDatagramProtocol is the AFV-Native absolute max datagram size.
const MaxDatagramProtocol = 65536

// SequenceWindowSize is the server RX anti-replay window (full uint64 bitfield).
const SequenceWindowSize = 64

// DTO wire names.
const (
	DTONameHeartbeat    = "H"
	DTONameHeartbeatAck = "HA"
	DTONameAudioTx      = "AT"
	DTONameAudioRx      = "AR"
)
