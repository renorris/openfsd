# VATSIM Auth (in-band client authenticity)

Separate from [JWT login tokens](authentication-token.md).

## Overview

VATSIM employs a bidirectional obfuscation scheme to “verify” that both ends of an FSD connection know a client-software secret.

Every VATSIM-approved client is assigned:

- a unique **unsigned 16-bit** client software ID, and  
- a **32-character** private key string (hex or alphanumeric; treated as 32 raw bytes of ASCII).

There are two parties: the client and the server.
When an FSD connection is established, each side sends random data in:

- [Server Identification](protocol.md#server-identification-di) (`$DI`) — server → client  
- [Client Identification](protocol.md#client-identification-id) (`$ID`) — client → server  

Using the client ID, private key, and those random values, each side constructs an **Auth State**.

## Auth State

```go
type AuthState struct {
	clientID  uint16 // assigned client software ID
	initState string // 32-char hex of initial MD5 (16 bytes)
	currState string // 32-char hex of current MD5 (16 bytes)
}
```

In openfsd the MD5 digests are stored as `[16]byte` and hex-encoded when mixed with challenges.

## Auth State construction

After `$DI` and `$ID` have been exchanged:

1. Set **Client ID** to the static software ID from `$ID` (same ID for both the server-side and client-side state machines for that connection).
2. Set the working key buffer to the client’s **private key** (32 ASCII bytes).
3. Each side runs one round of the obfuscation scheme using the **random data it received from the other side** as the challenge:
   - Server uses the client’s `$ID` initial challenge.
   - Client uses the server’s `$DI` initial challenge.
4. Set **both** Initial State and Current State to the 16-byte MD5 result of that round (stored/used as 32-char hex when forming subsequent challenges).

## Obfuscation scheme

**Inputs:** AuthState, challenge string (arbitrary length; typically hex).  
**Output:** 16-byte MD5 digest (usually hex-encoded to 32 chars on the wire).

```go
func (state *AuthState) ObfuscationScheme(challenge string) string {
	// Split the challenge into two halves (byte mid-point).
	c1, c2 := challenge[:(len(challenge)/2)], challenge[(len(challenge)/2):]

	// If the Client ID is odd, swap the two halves.
	if (state.clientID & 1) == 1 {
		c1, c2 = c2, c1
	}

	// Split the current state (32 ASCII hex chars) into three parts.
	s1, s2, s3 := state.currState[0:12], state.currState[12:22], state.currState[22:32]

	var h string
	switch state.clientID % 3 {
	case 0:
		h = s1 + c1 + s2 + c2 + s3
	case 1:
		h = s2 + c1 + s3 + c2 + s1
	default:
		h = s3 + c1 + s1 + c2 + s2
	}

	sum := md5.Sum([]byte(h))
	return hex.EncodeToString(sum[:]) // 32 hex chars
}
```

This matches openfsd `internal/auth/vatsim.go` (`runObfuscationRound`).

## Interrogations

After initial states are constructed, either side may challenge the other with [Auth Challenge](protocol.md#auth-challenge-zc) (`$ZC`) / [Auth Response](protocol.md#auth-response-zr) (`$ZR`):

1. `$ZC` carries a random challenge string (typically hex).
2. The **receiver** feeds (current state, challenge) into one obfuscation round.
3. The receiver sends the 32-char hex result in `$ZR`.
4. **Both** sides that care about verification then advance state:
   - Concatenate `initStateHex ‖ responseHex` → 64 ASCII bytes.
   - `currState = MD5(that 64-byte buffer)` (stored as 16 bytes / 32 hex chars).
5. The next challenge uses the updated current state.

The side that **issued** the challenge must maintain a mirror of the peer’s Auth State to verify `$ZR` values. openfsd answers client `$ZC` with `$ZRSERVER:…` and updates state via `UpdateState`.

## Known clients (software keys)

These IDs/keys are present in openfsd’s allowlist (`internal/auth/vatsim.go`).  
They are not secrets that protect user credentials (JWT does that); they only prove knowledge of a client-software key.

| Client ID | Private key (32 chars)                 | Client name   | Notes |
|-----------|----------------------------------------|---------------|--------|
| `8464`    | `945507c4c50222c34687e742729252e6`     | vSTARS        | In openfsd allowlist |
| `10452`   | `0ad74157c7f449c216bfed04f3af9fb9`     | vERAM         | In openfsd allowlist |
| `24515`   | `3424cbcebcca6fe95f973b350ff85cef`     | vatSys        | In openfsd allowlist |
| `27095`   | `3518a62c421937ffa46ac3316957da43`     | Euroscope     | In openfsd allowlist |
| `33456`   | `52d9343020e9c7d0c6b04b0cca20ad3b`     | swift         | In openfsd allowlist |
| `35044`   | `fe28334fb753cf0e3d19942197b9ce3e`     | vPilot        | In openfsd allowlist |
| `48312`   | `bc2eb1ef4d96709c683084055dd5e83f`     | TWRTrainer    | In openfsd allowlist |
| `55538`   | `ImuL1WbbhVuD8d3MuKpWn2rrLZRa9iVP`     | xPilot        | Alphanumeric key |
| `56862`   | `3518a62c421937ffa46ac3316957da43`     | VRC           | Same key material as Euroscope in this table |
| `2`       | `079f83e7d0fb6a9d99114439b2ea28fb`     | SquawkBox     | **Historical**; not in openfsd allowlist |

Unknown client IDs produce openfsd’s unauthorized-software / failed-auth path.

## Relation to protocol revision

Modern ATC clients commonly connect with protocol revision **100** on `#AA`.  
Pilot clients that support VATSIM Velocity use **101** and participate in `$SF` / fast positions (`^`, `#SL`, `#ST`).  
Auth challenge exchange is independent of that revision number but is part of the same post-handshake session.
